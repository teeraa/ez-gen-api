package handler_open_api

import (
	"errors"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// MarketplaceHandler serves the Marketplace menu under /marketplace.
type MarketplaceHandler struct {
	facade *facade_open_api.MarketplaceFacade
	aiRate int
}

func NewMarketplaceHandler(facade *facade_open_api.MarketplaceFacade, aiRatePerMinute int) *MarketplaceHandler {
	if aiRatePerMinute < 1 {
		aiRatePerMinute = 6
	}
	return &MarketplaceHandler{facade: facade, aiRate: aiRatePerMinute}
}

// RegisterRoutes mounts on the authenticated /open/v1 router.
func (h *MarketplaceHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/marketplace")
	// Suggestions call the text model synchronously and are free of charge, so they get their own per-user limit.
	ai := limiter.New(limiter.Config{Max: h.aiRate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return "marketplace-ai:" + userIDOf(c) }, LimitReached: func(c *fiber.Ctx) error {
		c.Set("Retry-After", "60")
		return utils.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "ขอคำแนะนำจาก AI ถี่เกินไป กรุณารอสักครู่", nil)
	}})

	g.Get("/shops", h.ListShops)
	g.Post("/shops", h.CreateShop)
	g.Get("/shops/:id", h.GetShop)
	g.Put("/shops/:id", h.UpdateShop)
	g.Delete("/shops/:id", h.DeleteShop)
	g.Post("/shops/:id/trends", ai, h.SuggestTrends)
	g.Get("/shops/:id/products", h.ListProducts)
	g.Post("/shops/:id/products", h.CreateProduct)

	g.Get("/products/:id", h.GetProduct)
	g.Put("/products/:id", h.UpdateProduct)
	g.Delete("/products/:id", h.DeleteProduct)
	g.Post("/products/:id/suggestions", ai, h.SuggestForProduct)
	g.Get("/products/:id/contents", h.ListContents)
	g.Post("/products/:id/contents", h.CreateContent)
	g.Get("/products/:id/publishes", h.ListPublishes)
	g.Post("/products/:id/publishes", h.CreatePublish)
	g.Get("/products/:id/sales", h.GetSales)
	g.Post("/products/:id/sales", h.UpsertSales)

	g.Delete("/contents/:id", h.DeleteContent)
	g.Delete("/publishes/:id", h.DeletePublish)
	g.Delete("/sales/:id", h.DeleteSale)
}

func marketplaceError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, facade_open_api.ErrMarketplaceLimit):
		return utils.Error(c, fiber.StatusConflict, "MARKETPLACE_LIMIT_REACHED", "จำนวนรายการครบขีดจำกัดแล้ว กรุณาลบรายการที่ไม่ใช้ก่อน", nil)
	case errors.Is(err, facade_open_api.ErrMarketplaceAIUnavailable):
		return utils.Error(c, fiber.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "ระบบยังไม่ได้ตั้งค่าโมเดลสำหรับคำแนะนำ AI กรุณาติดต่อผู้ดูแลระบบ", nil)
	case errors.Is(err, facade_open_api.ErrMarketplaceAIFailed):
		return utils.Error(c, fiber.StatusBadGateway, "AI_FAILED", "AI สร้างคำแนะนำไม่สำเร็จ กรุณาลองใหม่อีกครั้ง", nil)
	}
	return mapFacadeError(c, err)
}

// marketplaceBody parses and validates a JSON body; ok is false when a response was already sent.
func marketplaceBody(c *fiber.Ctx, req any) (ok bool, sent error) {
	if err := c.BodyParser(req); err != nil {
		return false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	return true, nil
}

func marketplaceQuery(c *fiber.Ctx) (facade_open_api.MarketplaceListQuery, []fieldError) {
	var q request_open_api.MarketplaceListQuery
	if err := c.QueryParser(&q); err != nil {
		return facade_open_api.MarketplaceListQuery{}, []fieldError{{Field: "query", Message: "is invalid"}}
	}
	if errs := validateRequest(q); errs != nil {
		return facade_open_api.MarketplaceListQuery{}, errs
	}
	page, limit := paginationParams(c)
	return facade_open_api.MarketplaceListQuery{Query: q.Q, Status: q.Status, Page: page, Limit: limit}, nil
}

// ── Shops ─────────────────────────────────────────────────────────────────────

func (h *MarketplaceHandler) ListShops(c *fiber.Ctx) error {
	q, errs := marketplaceQuery(c)
	if errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	list, err := h.facade.ListShops(c.Context(), userIDOf(c), q)
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func shopInput(req request_open_api.MarketplaceShopRequest) facade_open_api.MarketplaceShopInput {
	return facade_open_api.MarketplaceShopInput{
		Name: req.Name, Description: req.Description, Category: req.Category, LogoUploadID: req.LogoUploadID,
		Phone: req.Phone, Email: req.Email, LineID: req.LineID, Website: req.Website, Address: req.Address,
		BrandTone: req.BrandTone, TargetAudience: req.TargetAudience,
		TiktokShopURL: req.TiktokShopURL, ShopeeURL: req.ShopeeURL, LazadaURL: req.LazadaURL, Status: req.Status,
	}
}

func (h *MarketplaceHandler) CreateShop(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceShopRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.CreateShop(c.Context(), userIDOf(c), shopInput(req))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *MarketplaceHandler) GetShop(c *fiber.Ctx) error {
	dto, err := h.facade.GetShop(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *MarketplaceHandler) UpdateShop(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceShopRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.UpdateShop(c.Context(), c.Params("id"), userIDOf(c), shopInput(req))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *MarketplaceHandler) DeleteShop(c *fiber.Ctx) error {
	if err := h.facade.DeleteShop(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *MarketplaceHandler) SuggestTrends(c *fiber.Ctx) error {
	dto, err := h.facade.SuggestTrends(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

// ── Products ──────────────────────────────────────────────────────────────────

func (h *MarketplaceHandler) ListProducts(c *fiber.Ctx) error {
	q, errs := marketplaceQuery(c)
	if errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	list, err := h.facade.ListProducts(c.Context(), c.Params("id"), userIDOf(c), q)
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func productInput(req request_open_api.MarketplaceProductRequest) facade_open_api.MarketplaceProductInput {
	return facade_open_api.MarketplaceProductInput{
		Name: req.Name, Description: req.Description, SellingPoints: req.SellingPoints, Category: req.Category, SKU: req.SKU,
		Price: req.Price, Stock: req.Stock, Status: req.Status, ImageUploadIDs: req.ImageUploadIDs,
		TiktokShopURL: req.TiktokShopURL, ShopeeURL: req.ShopeeURL, LazadaURL: req.LazadaURL,
	}
}

func (h *MarketplaceHandler) CreateProduct(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceProductRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.CreateProduct(c.Context(), c.Params("id"), userIDOf(c), productInput(req))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *MarketplaceHandler) GetProduct(c *fiber.Ctx) error {
	dto, err := h.facade.GetProduct(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *MarketplaceHandler) UpdateProduct(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceProductRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.UpdateProduct(c.Context(), c.Params("id"), userIDOf(c), productInput(req))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *MarketplaceHandler) DeleteProduct(c *fiber.Ctx) error {
	if err := h.facade.DeleteProduct(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *MarketplaceHandler) SuggestForProduct(c *fiber.Ctx) error {
	dto, err := h.facade.SuggestForProduct(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

// ── Contents ──────────────────────────────────────────────────────────────────

func (h *MarketplaceHandler) ListContents(c *fiber.Ctx) error {
	items, err := h.facade.ListContents(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto_open_api.MarketplaceContentListDTO{Items: items})
}

func (h *MarketplaceHandler) CreateContent(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceContentRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.CreateContent(c.Context(), c.Params("id"), userIDOf(c), facade_open_api.MarketplaceContentInput{
		Kind: req.Kind, Title: req.Title, Brief: req.Brief, RefType: req.RefType, RefID: req.RefID,
	})
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *MarketplaceHandler) DeleteContent(c *fiber.Ctx) error {
	if err := h.facade.DeleteContent(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Publishes ─────────────────────────────────────────────────────────────────

func (h *MarketplaceHandler) ListPublishes(c *fiber.Ctx) error {
	items, err := h.facade.ListPublishes(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto_open_api.MarketplacePublishListDTO{Items: items})
}

func (h *MarketplaceHandler) CreatePublish(c *fiber.Ctx) error {
	var req request_open_api.MarketplacePublishRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	dto, err := h.facade.CreatePublish(c.Context(), c.Params("id"), userIDOf(c), facade_open_api.MarketplacePublishInput{
		ContentID: req.ContentID, Platform: req.Platform, Caption: req.Caption, PostURL: req.PostURL, PostedOn: req.PostedOn,
	})
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *MarketplaceHandler) DeletePublish(c *fiber.Ctx) error {
	if err := h.facade.DeletePublish(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Sales ─────────────────────────────────────────────────────────────────────

func (h *MarketplaceHandler) GetSales(c *fiber.Ctx) error {
	dto, err := h.facade.GetSales(c.Context(), c.Params("id"), userIDOf(c), c.QueryInt("days", 30))
	if err != nil {
		return marketplaceError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *MarketplaceHandler) UpsertSales(c *fiber.Ctx) error {
	var req request_open_api.MarketplaceSalesRequest
	if ok, sent := marketplaceBody(c, &req); !ok {
		return sent
	}
	rows := make([]facade_open_api.MarketplaceSaleInput, 0, len(req.Rows))
	for _, r := range req.Rows {
		rows = append(rows, facade_open_api.MarketplaceSaleInput{Platform: r.Platform, Date: r.Date, Units: r.Units, Orders: r.Orders, Revenue: r.Revenue})
	}
	if err := h.facade.UpsertSales(c.Context(), c.Params("id"), userIDOf(c), rows); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *MarketplaceHandler) DeleteSale(c *fiber.Ctx) error {
	if err := h.facade.DeleteSale(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return marketplaceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
