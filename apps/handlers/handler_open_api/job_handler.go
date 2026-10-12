package handler_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// JobHandler handles generation job endpoints.
type JobHandler struct {
	facade facade_open_api.JobFacade
}

func NewJobHandler(facade facade_open_api.JobFacade) *JobHandler {
	return &JobHandler{facade: facade}
}

// RegisterRoutes mounts job routes on the given router.
// Expects the router to already be scoped to /open/v1.
func (h *JobHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/jobs", h.CreateJob)
	r.Get("/jobs", h.ListJobs)
	r.Get("/jobs/:jobId", h.GetJob)
	r.Delete("/jobs/:jobId", h.CancelJob)
}

// RegisterSceneRoutes mounts the Continue Scene route.
// Called with the /open/v1/dna router.
func (h *JobHandler) RegisterSceneRoutes(r fiber.Router) {
	r.Post("/scenes/:id/continue", h.ContinueScene)
	r.Post("/scenes/:id/generate", h.GenerateForScene)
}

func (h *JobHandler) CreateJob(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	var req request_open_api.CreateJobRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.CreateJobInput{
		Prompt:              req.Prompt,
		Mode:                req.Mode,
		ModelID:             req.Model,
		AspectRatio:         req.AspectRatio,
		DurationSeconds:     req.Duration,
		Quality:             req.Quality,
		EnhancePrompt:       req.EnhancePrompt,
		UploadID:            req.ReferenceImageID,
		SceneID:             req.SceneID,
		CharacterProfileID:  req.CharacterProfileID,
		CharacterProfileIDs: req.CharacterProfileIDs,
		StyleProfileID:      req.StyleProfileID,
		TagIDs:              req.TagIDs,
		IdempotencyKey:      req.IdempotencyKey,
	}
	dto, err := h.facade.CreateJob(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

func (h *JobHandler) GetJob(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetJob(c.Context(), c.Params("jobId"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *JobHandler) ListJobs(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	page, limit := paginationParams(c)
	list, err := h.facade.ListJobs(c.Context(), userID, page, limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list jobs", nil)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

func (h *JobHandler) CancelJob(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.CancelJob(c.Context(), c.Params("jobId"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

func (h *JobHandler) ContinueScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	sceneID := c.Params("id")
	var req request_open_api.ContinueSceneRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.ContinueSceneInput{
		Title:           req.Title,
		ActionOverride:  req.ActionOverride,
		CameraOverride:  req.CameraOverride,
		SettingOverride: req.SettingOverride,
		ModelID:         req.Model,
		AspectRatio:     req.AspectRatio,
		DurationSeconds: req.Duration,
		Quality:         req.Quality,
		UseLastFrame:    req.UseLastFrame,
		IdempotencyKey:  req.IdempotencyKey,
	}
	dto, err := h.facade.ContinueScene(c.Context(), sceneID, userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

// GenerateForScene creates a job directly for an existing scene
// (without continuing — just generates with the scene's current profile state).
func (h *JobHandler) GenerateForScene(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	sceneID := c.Params("id")
	var req request_open_api.CreateJobRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}
	in := facade_open_api.CreateJobInput{
		Prompt:              req.Prompt,
		Mode:                req.Mode,
		ModelID:             req.Model,
		AspectRatio:         req.AspectRatio,
		DurationSeconds:     req.Duration,
		Quality:             req.Quality,
		EnhancePrompt:       req.EnhancePrompt,
		UploadID:            req.ReferenceImageID,
		SceneID:             &sceneID,
		CharacterProfileID:  req.CharacterProfileID,
		CharacterProfileIDs: req.CharacterProfileIDs,
		StyleProfileID:      req.StyleProfileID,
		TagIDs:              req.TagIDs,
		IdempotencyKey:      req.IdempotencyKey,
	}
	dto, err := h.facade.CreateJob(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}
