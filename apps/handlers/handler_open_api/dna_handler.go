package handler_open_api

import (
	"errors"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// DNAHandler handles all VIOLET DNA HTTP endpoints.
type DNAHandler struct {
	facade facade_open_api.DNAFacade
}

func NewDNAHandler(facade facade_open_api.DNAFacade) *DNAHandler {
	return &DNAHandler{facade: facade}
}

// RegisterRoutes mounts all DNA routes on the given router.
// Expects /open/v1/dna as the router prefix.
func (h *DNAHandler) RegisterRoutes(r fiber.Router) {
	// Tags
	r.Get("/tags", h.ListTags)
	r.Get("/tags/categories", h.ListTagCategories)

	// Character profiles
	r.Post("/characters", h.CreateCharacterProfile)
	r.Get("/characters", h.ListCharacterProfiles)
	r.Get("/characters/:id", h.GetCharacterProfile)
	r.Put("/characters/:id", h.UpdateCharacterProfile)
	r.Delete("/characters/:id", h.DeleteCharacterProfile)

	// Style profiles
	r.Post("/styles", h.CreateStyleProfile)
	r.Get("/styles", h.ListStyleProfiles)
	r.Get("/styles/:id", h.GetStyleProfile)
	r.Put("/styles/:id", h.UpdateStyleProfile)
	r.Delete("/styles/:id", h.DeleteStyleProfile)

	// Scenes
	r.Post("/scenes", h.CreateScene)
	r.Get("/scenes", h.ListScenes)
	r.Get("/scenes/:id", h.GetScene)
	r.Put("/scenes/:id", h.UpdateScene)
	r.Delete("/scenes/:id", h.DeleteScene)
	r.Put("/scenes/:id/select-output/:jobId", h.SelectSceneOutput)
}

// ── Tags ──────────────────────────────────────────────────────────────────────

func (h *DNAHandler) ListTags(c *fiber.Ctx) error {
	var q request_open_api.ListTagsQuery
	if err := c.QueryParser(&q); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid query parameters", nil)
	}
	dtos, err := h.facade.ListTags(c.Context(), q.Category)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list tags", nil)
	}
	return utils.Success(c, fiber.StatusOK, dtos)
}

func (h *DNAHandler) ListTagCategories(c *fiber.Ctx) error {
	cats := h.facade.ListTagCategories(c.Context())
	return utils.Success(c, fiber.StatusOK, cats)
}

// ── Character Profiles ────────────────────────────────────────────────────────

func (h *DNAHandler) CreateCharacterProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.CreateCharacterProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.CreateCharacterProfileInput{
		Name:             req.Name,
		Description:      req.Description,
		Appearance:       req.Appearance,
		Hairstyle:        req.Hairstyle,
		Clothing:         req.Clothing,
		Accessories:      req.Accessories,
		DefiningFeatures: req.DefiningFeatures,
		ConsistencyNotes: req.ConsistencyNotes,
		TagIDs:           req.TagIDs,
	}
	dto, err := h.facade.CreateCharacterProfile(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *DNAHandler) GetCharacterProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetCharacterProfile(c.Context(), c.Params("id"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) ListCharacterProfiles(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	page, limit := paginationParams(c)
	list, err := h.facade.ListCharacterProfiles(c.Context(), userID, page, limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list character profiles", nil)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func (h *DNAHandler) UpdateCharacterProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.UpdateCharacterProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.UpdateCharacterProfileInput{
		Name:             req.Name,
		Description:      req.Description,
		Appearance:       req.Appearance,
		Hairstyle:        req.Hairstyle,
		Clothing:         req.Clothing,
		Accessories:      req.Accessories,
		DefiningFeatures: req.DefiningFeatures,
		ConsistencyNotes: req.ConsistencyNotes,
		TagIDs:           req.TagIDs,
	}
	dto, err := h.facade.UpdateCharacterProfile(c.Context(), c.Params("id"), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) DeleteCharacterProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	if err := h.facade.DeleteCharacterProfile(c.Context(), c.Params("id"), userID); err != nil {
		return mapFacadeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Style Profiles ────────────────────────────────────────────────────────────

func (h *DNAHandler) CreateStyleProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.CreateStyleProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.CreateStyleProfileInput{
		Name:           req.Name,
		Description:    req.Description,
		VisualStyle:    req.VisualStyle,
		ColorPalette:   req.ColorPalette,
		Lighting:       req.Lighting,
		Mood:           req.Mood,
		CameraLanguage: req.CameraLanguage,
		QualityNotes:   req.QualityNotes,
		TagIDs:         req.TagIDs,
	}
	dto, err := h.facade.CreateStyleProfile(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *DNAHandler) GetStyleProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetStyleProfile(c.Context(), c.Params("id"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) ListStyleProfiles(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	page, limit := paginationParams(c)
	list, err := h.facade.ListStyleProfiles(c.Context(), userID, page, limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list style profiles", nil)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func (h *DNAHandler) UpdateStyleProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.UpdateStyleProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.UpdateStyleProfileInput{
		Name:           req.Name,
		Description:    req.Description,
		VisualStyle:    req.VisualStyle,
		ColorPalette:   req.ColorPalette,
		Lighting:       req.Lighting,
		Mood:           req.Mood,
		CameraLanguage: req.CameraLanguage,
		QualityNotes:   req.QualityNotes,
		TagIDs:         req.TagIDs,
	}
	dto, err := h.facade.UpdateStyleProfile(c.Context(), c.Params("id"), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) DeleteStyleProfile(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	if err := h.facade.DeleteStyleProfile(c.Context(), c.Params("id"), userID); err != nil {
		return mapFacadeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Scenes ────────────────────────────────────────────────────────────────────

func (h *DNAHandler) CreateScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.CreateSceneRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.CreateSceneInput{
		Title:              req.Title,
		Description:        req.Description,
		CharacterProfileID: req.CharacterProfileID,
		StyleProfileID:     req.StyleProfileID,
		PreviousSceneID:    req.PreviousSceneID,
		SceneOrder:         req.SceneOrder,
		ActionOverride:     req.ActionOverride,
		CameraOverride:     req.CameraOverride,
		SettingOverride:    req.SettingOverride,
		TagIDs:             req.TagIDs,
	}
	dto, err := h.facade.CreateScene(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *DNAHandler) GetScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetScene(c.Context(), c.Params("id"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) ListScenes(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	page, limit := paginationParams(c)
	list, err := h.facade.ListScenes(c.Context(), userID, page, limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list scenes", nil)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func (h *DNAHandler) UpdateScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.UpdateSceneRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.UpdateSceneInput{
		Title:              req.Title,
		Description:        req.Description,
		CharacterProfileID: req.CharacterProfileID,
		StyleProfileID:     req.StyleProfileID,
		SceneOrder:         req.SceneOrder,
		ActionOverride:     req.ActionOverride,
		CameraOverride:     req.CameraOverride,
		SettingOverride:    req.SettingOverride,
		TagIDs:             req.TagIDs,
	}
	dto, err := h.facade.UpdateScene(c.Context(), c.Params("id"), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *DNAHandler) DeleteScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	if err := h.facade.DeleteScene(c.Context(), c.Params("id"), userID); err != nil {
		return mapFacadeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *DNAHandler) SelectSceneOutput(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	sceneID := c.Params("id")
	jobID := c.Params("jobId")
	if err := h.facade.SelectSceneOutput(c.Context(), sceneID, jobID, userID); err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, fiber.Map{"sceneId": sceneID, "selectedJobId": jobID})
}

// ─── shared helpers ───────────────────────────────────────────────────────────

func mapFacadeError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, facade_open_api.ErrNotFound):
		return utils.Error(c, fiber.StatusNotFound, "NOT_FOUND", "resource not found", nil)
	case errors.Is(err, facade_open_api.ErrUnauthorized):
		return utils.Error(c, fiber.StatusUnauthorized, "UNAUTHORIZED", "unauthorized", nil)
	case errors.Is(err, facade_open_api.ErrConflict):
		return utils.Error(c, fiber.StatusConflict, "CONFLICT", "conflict with current state", nil)
	case errors.Is(err, facade_open_api.ErrRateLimit):
		return utils.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "too many concurrent jobs", nil)
	case errors.Is(err, facade_open_api.ErrImageRequired):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "IMAGE_REQUIRED", "mode requires a reference image", nil)
	case errors.Is(err, facade_open_api.ErrImageExpired):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "IMAGE_EXPIRED", "reference image has expired", nil)
	case errors.Is(err, facade_open_api.ErrModelInvalid):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "MODEL_NOT_SUPPORTED", "model does not support the requested mode", nil)
	case errors.Is(err, facade_open_api.ErrProviderError):
		return utils.Error(c, fiber.StatusBadGateway, "PROVIDER_ERROR", "generation provider returned an error", nil)
	case errors.Is(err, facade_open_api.ErrValidation):
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid input", nil)
	default:
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred", nil)
	}
}

func paginationParams(c *fiber.Ctx) (page, limit int) {
	page = c.QueryInt("page", 1)
	limit = c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}
	return page, limit
}
