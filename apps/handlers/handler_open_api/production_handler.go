package handler_open_api

import (
	"context"
	"database/sql"
	"errors"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"log"
	"net"
	"strings"
)

type ProductionHandler struct{ facade *f.ProductionFacade }

func NewProductionHandler(p *f.ProductionFacade) *ProductionHandler { return &ProductionHandler{p} }
func (h *ProductionHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/video-capabilities", func(c *fiber.Ctx) error { return utils.Success(c, 200, h.facade.Capabilities()) })
	r.Get("/projects", h.List)
	r.Post("/projects", h.Create)
	r.Get("/projects/:id", h.Detail)
	r.Put("/projects/:id", h.Update)
	r.Delete("/projects/:id", h.Delete)
	r.Get("/projects/:id/scenes", h.Scenes)
	r.Post("/projects/:id/scenes", h.CreateScene)
	r.Put("/projects/:id/scenes/order", h.Reorder)
	r.Put("/projects/:id/editor", h.Editor)
	r.Post("/dna/scenes/:id/duplicate", h.Duplicate)
	r.Get("/dna/scenes/:id/jobs", h.Jobs)
}
func productionUser(c *fiber.Ctx) string { return c.Locals("userID").(string) }
func productionBody(c *fiber.Ctx, v any) error {
	if c.BodyParser(v) != nil {
		return f.ErrValidation
	}
	if validateRequest(v) != nil {
		return f.ErrValidation
	}
	return nil
}
func (h *ProductionHandler) Create(c *fiber.Ctx) error {
	var in req.CreateProjectRequest
	if e := productionBody(c, &in); e != nil {
		return mapFacadeError(c, e)
	}
	d, e := h.facade.Create(c.Context(), productionUser(c), in)
	if e != nil {
		reportProductionError(c, "projects.create", e)
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 201, d)
}
func (h *ProductionHandler) List(c *fiber.Ctx) error {
	p, l := paginationParams(c)
	d, e := h.facade.List(c.Context(), productionUser(c), p, l)
	if e != nil {
		reportProductionError(c, "projects.list", e)
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) Detail(c *fiber.Ctx) error {
	d, e := h.facade.Detail(c.Context(), c.Params("id"), productionUser(c))
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) Update(c *fiber.Ctx) error {
	var in req.UpdateProjectRequest
	if e := productionBody(c, &in); e != nil {
		return mapFacadeError(c, e)
	}
	d, e := h.facade.Update(c.Context(), c.Params("id"), productionUser(c), in)
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) Delete(c *fiber.Ctx) error {
	if e := h.facade.Delete(c.Context(), c.Params("id"), productionUser(c)); e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, fiber.Map{"deleted": true})
}
func (h *ProductionHandler) Scenes(c *fiber.Ctx) error {
	d, e := h.facade.ListScenes(c.Context(), c.Params("id"), productionUser(c))
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) CreateScene(c *fiber.Ctx) error {
	var in req.CreateProductionSceneRequest
	if e := productionBody(c, &in); e != nil {
		return mapFacadeError(c, e)
	}
	d, e := h.facade.CreateScene(c.Context(), c.Params("id"), productionUser(c), in)
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 201, d)
}
func (h *ProductionHandler) Reorder(c *fiber.Ctx) error {
	var in req.ReorderScenesRequest
	if e := productionBody(c, &in); e != nil {
		return mapFacadeError(c, e)
	}
	d, e := h.facade.Reorder(c.Context(), c.Params("id"), productionUser(c), in.SceneIDs)
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) Editor(c *fiber.Ctx) error {
	var in req.SaveEditorRequest
	if e := productionBody(c, &in); e != nil {
		return mapFacadeError(c, e)
	}
	d, e := h.facade.SaveEditor(c.Context(), c.Params("id"), productionUser(c), in)
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *ProductionHandler) Duplicate(c *fiber.Ctx) error {
	d, e := h.facade.Duplicate(c.Context(), c.Params("id"), productionUser(c))
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 201, d)
}
func (h *ProductionHandler) Jobs(c *fiber.Ctx) error {
	p, l := paginationParams(c)
	d, e := h.facade.SceneJobs(c.Context(), c.Params("id"), productionUser(c), p, l)
	if e != nil {
		return mapFacadeError(c, e)
	}
	return utils.Success(c, 200, d)
}

func reportProductionError(c *fiber.Ctx, operation string, err error) {
	requestID, _ := c.Locals("requestID").(string)
	if len(requestID) > 64 {
		requestID = requestID[:64]
	}
	category, state := productionErrorClassification(err)
	// Database errors can contain SQL, prompts and credentials; log only allowlisted fields.
	log.Printf("[production] operation=%s category=%s sqlstate=%s requestId=%q", operation, category, state, requestID)
}

func productionErrorClassification(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", "none"
	}
	var stateError interface{ SQLState() string }
	if errors.As(err, &stateError) {
		state := stateError.SQLState()
		valid := len(state) == 5
		for _, ch := range state {
			if !(ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z') {
				valid = false
			}
		}
		if valid {
			if strings.HasPrefix(state, "08") {
				return "connection", state
			}
			return "database", state
		}
	}
	var networkError net.Error
	if errors.Is(err, sql.ErrConnDone) || errors.As(err, &networkError) {
		return "connection", "none"
	}
	return "internal", "none"
}
