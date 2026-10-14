package handler_open_api

import (
	"errors"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

const maxUploadBodyBytes = 55 << 20 // 55 MB — covers 50 MB audio + form overhead

// UploadHandler handles POST /open/v1/uploads.
type UploadHandler struct {
	facade facade_open_api.UploadFacade
}

func NewUploadHandler(facade facade_open_api.UploadFacade) *UploadHandler {
	return &UploadHandler{facade: facade}
}

func (h *UploadHandler) RegisterRoutes(r fiber.Router) {
	r.Post("/uploads", h.CreateUpload)
}

// CreateUpload accepts a multipart/form-data file upload (image or audio).
// The file field must be named "file".
func (h *UploadHandler) CreateUpload(c *fiber.Ctx) error {
	userID := c.Locals("userID").(string)

	// Limit request size before parsing multipart.
	if cl := c.Request().Header.ContentLength(); cl > 0 && cl > maxUploadBodyBytes {
		return utils.Error(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "file exceeds size limit", nil)
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "VALIDATION_ERROR", "multipart field 'file' is required", nil)
	}

	f, err := file.Open()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file", nil)
	}
	defer f.Close()

	data := make([]byte, file.Size)
	if _, err := f.Read(data); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file", nil)
	}

	dto, err := h.facade.CreateUpload(c.Context(), userID, facade_open_api.CreateUploadInput{
		Filename: file.Filename,
		Data:     data,
		MimeType: file.Header.Get("Content-Type"),
	})
	if err != nil {
		if errors.Is(err, facade_open_api.ErrInvalidFileType) {
			return utils.Error(c, fiber.StatusBadRequest, "INVALID_FILE_TYPE", "unsupported file type; accepted: JPEG, PNG, WebP, MP3, AAC, WAV", nil)
		}
		if errors.Is(err, facade_open_api.ErrFileTooLarge) {
			return utils.Error(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "file exceeds size limit", nil)
		}
		return utils.Error(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", "failed to upload file", nil)
	}
	return utils.Success(c, fiber.StatusCreated, dto)
}
