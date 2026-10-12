package handler_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// ShareHandler serves share links (owner, authenticated) and the public share page data.
type ShareHandler struct {
	facade facade_open_api.ShareFacade
}

func NewShareHandler(facade facade_open_api.ShareFacade) *ShareHandler {
	return &ShareHandler{facade: facade}
}

// RegisterRoutes mounts owner routes on the authenticated /open/v1 router.
func (h *ShareHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/jobs/:jobId/shares", h.CreateShare)
	r.Get("/jobs/:jobId/download", h.GetDownloadURL)
	r.Delete("/shares/:shareId", h.RevokeShare)
}

// RegisterPublicRoutes mounts unauthenticated routes on the /public/v1 router.
func (h *ShareHandler) RegisterPublicRoutes(r fiber.Router) {
	r.Get("/shares/:token", h.GetPublicShare)
}

func (h *ShareHandler) CreateShare(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, created, err := h.facade.CreateShare(c.Context(), userID, c.Params("jobId"))
	if err != nil {
		return mapFacadeError(c, err)
	}
	status := fiber.StatusOK
	if created {
		status = fiber.StatusCreated
	}
	return utils.Success(c, status, dto)
}

func (h *ShareHandler) RevokeShare(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	if err := h.facade.RevokeShare(c.Context(), userID, c.Params("shareId")); err != nil {
		return mapFacadeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *ShareHandler) GetDownloadURL(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetDownloadURL(c.Context(), userID, c.Params("jobId"))
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *ShareHandler) GetPublicShare(c *fiber.Ctx) error {
	dto, err := h.facade.GetPublicShare(c.Context(), c.Params("token"))
	if err != nil {
		return mapFacadeError(c, err)
	}
	// Short enough that a revoked link disappears quickly, long enough that a link
	// going viral is served from the frontend/CDN cache rather than this API.
	c.Set(fiber.HeaderCacheControl, "public, max-age=60")
	return utils.Success(c, fiber.StatusOK, dto)
}
