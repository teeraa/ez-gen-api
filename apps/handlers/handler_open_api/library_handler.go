package handler_open_api

import (
	"errors"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// LibraryHandler serves the Prompt library (/prompts) and Skills (/skills) menus.
type LibraryHandler struct {
	facade facade_open_api.LibraryFacade
}

func NewLibraryHandler(facade facade_open_api.LibraryFacade) *LibraryHandler {
	return &LibraryHandler{facade: facade}
}

// RegisterRoutes mounts on the authenticated /api/v1 router.
func (h *LibraryHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/prompts", h.ListPrompts)
	r.Post("/prompts", h.CreatePrompt)
	r.Get("/prompts/:id", h.GetPrompt)
	r.Put("/prompts/:id", h.UpdatePrompt)
	r.Delete("/prompts/:id", h.DeletePrompt)
	r.Put("/prompts/:id/bookmark", h.BookmarkPrompt)
	r.Delete("/prompts/:id/bookmark", h.UnbookmarkPrompt)

	r.Get("/skills", h.ListSkills)
	r.Post("/skills", h.CreateSkill)
	r.Get("/skills/:id", h.GetSkill)
	r.Put("/skills/:id", h.UpdateSkill)
	r.Delete("/skills/:id", h.DeleteSkill)
}

func libraryError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, facade_open_api.ErrBuiltInReadOnly):
		return utils.Error(c, fiber.StatusForbidden, "BUILT_IN_READ_ONLY", "รายการแนะนำของระบบแก้ไขหรือลบไม่ได้ ให้ทำสำเนาแล้วแก้แทน", nil)
	case errors.Is(err, facade_open_api.ErrLibraryLimit):
		return utils.Error(c, fiber.StatusConflict, "LIBRARY_LIMIT_REACHED", "จำนวนรายการของคุณครบขีดจำกัดแล้ว กรุณาลบรายการที่ไม่ใช้ก่อน", nil)
	}
	return mapFacadeError(c, err)
}

func libraryQuery(c *fiber.Ctx) (facade_open_api.LibraryQuery, []fieldError) {
	var q request_open_api.ListLibraryQuery
	if err := c.QueryParser(&q); err != nil {
		return facade_open_api.LibraryQuery{}, []fieldError{{Field: "query", Message: "is invalid"}}
	}
	if errs := validateRequest(q); errs != nil {
		return facade_open_api.LibraryQuery{}, errs
	}
	page, limit := paginationParams(c)
	scope := q.Scope
	if scope == "" {
		scope = "all"
	}
	return facade_open_api.LibraryQuery{Scope: scope, Category: q.Category, Query: q.Q, Output: q.Output, Page: page, Limit: limit}, nil
}

func userIDOf(c *fiber.Ctx) string {
	id, _ := c.Locals("userID").(string)
	return id
}

// ── Prompts ───────────────────────────────────────────────────────────────────

func (h *LibraryHandler) ListPrompts(c *fiber.Ctx) error {
	q, errs := libraryQuery(c)
	if errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	list, err := h.facade.ListPrompts(c.Context(), userIDOf(c), q)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func parsePromptBody(c *fiber.Ctx) (facade_open_api.PromptInput, bool, error) {
	var req request_open_api.PromptRequest
	if err := c.BodyParser(&req); err != nil {
		return facade_open_api.PromptInput{}, false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return facade_open_api.PromptInput{}, false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	return facade_open_api.PromptInput{Title: req.Title, Category: req.Category, Body: req.Body}, true, nil
}

func (h *LibraryHandler) CreatePrompt(c *fiber.Ctx) error {
	in, ok, sent := parsePromptBody(c)
	if !ok {
		return sent
	}
	dto, err := h.facade.CreatePrompt(c.Context(), userIDOf(c), in)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *LibraryHandler) GetPrompt(c *fiber.Ctx) error {
	dto, err := h.facade.GetPrompt(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *LibraryHandler) UpdatePrompt(c *fiber.Ctx) error {
	in, ok, sent := parsePromptBody(c)
	if !ok {
		return sent
	}
	dto, err := h.facade.UpdatePrompt(c.Context(), c.Params("id"), userIDOf(c), in)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *LibraryHandler) DeletePrompt(c *fiber.Ctx) error {
	if err := h.facade.DeletePrompt(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return libraryError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *LibraryHandler) BookmarkPrompt(c *fiber.Ctx) error   { return h.setBookmark(c, true) }
func (h *LibraryHandler) UnbookmarkPrompt(c *fiber.Ctx) error { return h.setBookmark(c, false) }

func (h *LibraryHandler) setBookmark(c *fiber.Ctx, on bool) error {
	dto, err := h.facade.SetPromptBookmark(c.Context(), c.Params("id"), userIDOf(c), on)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

// ── Skills ────────────────────────────────────────────────────────────────────

func (h *LibraryHandler) ListSkills(c *fiber.Ctx) error {
	q, errs := libraryQuery(c)
	if errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	list, err := h.facade.ListSkills(c.Context(), userIDOf(c), q)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func parseSkillBody(c *fiber.Ctx) (facade_open_api.SkillInput, bool, error) {
	var req request_open_api.SkillRequest
	if err := c.BodyParser(&req); err != nil {
		return facade_open_api.SkillInput{}, false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return facade_open_api.SkillInput{}, false, utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	return facade_open_api.SkillInput{
		Name: req.Name, Description: req.Description, Steps: req.Steps, Inputs: req.Inputs,
		Output: req.Output, Directive: req.Directive, ForkedFromID: req.ForkedFromID,
	}, true, nil
}

func (h *LibraryHandler) CreateSkill(c *fiber.Ctx) error {
	in, ok, sent := parseSkillBody(c)
	if !ok {
		return sent
	}
	dto, err := h.facade.CreateSkill(c.Context(), userIDOf(c), in)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *LibraryHandler) GetSkill(c *fiber.Ctx) error {
	dto, err := h.facade.GetSkill(c.Context(), c.Params("id"), userIDOf(c))
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *LibraryHandler) UpdateSkill(c *fiber.Ctx) error {
	in, ok, sent := parseSkillBody(c)
	if !ok {
		return sent
	}
	in.ForkedFromID = nil
	dto, err := h.facade.UpdateSkill(c.Context(), c.Params("id"), userIDOf(c), in)
	if err != nil {
		return libraryError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *LibraryHandler) DeleteSkill(c *fiber.Ctx) error {
	if err := h.facade.DeleteSkill(c.Context(), c.Params("id"), userIDOf(c)); err != nil {
		return libraryError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
