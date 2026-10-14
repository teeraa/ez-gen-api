package handler_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// ExportHandler handles storyboard export endpoints.
type ExportHandler struct {
	facade facade_open_api.ExportFacade
}

func NewExportHandler(facade facade_open_api.ExportFacade) *ExportHandler {
	return &ExportHandler{facade: facade}
}

func (h *ExportHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/exports", h.CreateExport)
	r.Get("/exports", h.ListExports)
	r.Get("/exports/:exportId", h.GetExport)
	r.Get("/exports/:exportId/download", h.GetExportDownload)
}

// CreateExport queues a new storyboard export.
func (h *ExportHandler) CreateExport(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)

	var req request_open_api.CreateExportRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", nil)
	}
	if errs := validateRequest(req); errs != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "validation failed", errs)
	}

	in := facade_open_api.CreateExportInput{
		SceneIDs:      req.SceneIDs,
		AudioUploadID: req.AudioUploadID,
	}
	if req.AudioVolume != nil {
		in.AudioVolume = *req.AudioVolume
	}
	for _, tl := range req.TextLayers {
		layer := facade_open_api.ExportTextLayer{
			SceneID:  tl.SceneID,
			Text:     tl.Text,
			Position: tl.Position,
		}
		if tl.FontSize != nil {
			layer.FontSize = *tl.FontSize
		}
		if tl.Color != nil {
			layer.Color = *tl.Color
		}
		in.TextLayers = append(in.TextLayers, layer)
	}

	dto, err := h.facade.CreateExport(c.Context(), userID, in)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}

// GetExport returns the status of a single export.
func (h *ExportHandler) GetExport(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetExport(c.Context(), c.Params("exportId"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}

// ListExports returns paginated exports for the authenticated user.
func (h *ExportHandler) ListExports(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	page, limit := paginationParams(c)
	list, err := h.facade.ListExports(c.Context(), userID, page, limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to list exports", nil)
	}
	return utils.Success(c, fiber.StatusOK, list)
}

// GetExportDownload returns a short-lived signed download URL for a finished export.
func (h *ExportHandler) GetExportDownload(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)
	dto, err := h.facade.GetDownloadURL(c.Context(), c.Params("exportId"), userID)
	if err != nil {
		return mapFacadeError(c, err)
	}
	return utils.Success(c, fiber.StatusOK, dto)
}
