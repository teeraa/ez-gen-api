package handler_open_api

import (
	"bytes"
	"errors"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"io"
	"log"
	"mime"
	"strconv"
	"sync"
)

type VideoEditorHandler struct {
	facade  *f.VideoEditorFacade
	mu      sync.Mutex
	uploads map[string]int
}

func NewVideoEditorHandler(facade *f.VideoEditorFacade) *VideoEditorHandler {
	return &VideoEditorHandler{facade: facade, uploads: map[string]int{}}
}
func (h *VideoEditorHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/video-editor", func(c *fiber.Ctx) error {
		user, ok := c.Locals("userID").(string)
		if !ok {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		if _, err := uuid.Parse(user); err != nil {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		return c.Next()
	})
	g.Get("/capabilities", func(c *fiber.Ctx) error { return utils.Success(c, 200, h.facade.Capabilities()) })
	g.Get("/projects", h.list)
	g.Post("/projects", h.create)
	g.Get("/projects/:id", h.detail)
	g.Put("/projects/:id", h.save)
	g.Delete("/projects/:id", h.remove)
	g.Get("/projects/:id/assets", h.assets)
	g.Post("/projects/:id/assets", h.upload)
	// Chunked uploads for files above one request body (see video_editor_upload_sessions.go).
	g.Post("/projects/:id/voice", h.voice)
	g.Post("/projects/:id/uploads", h.uploadStart)
	g.Put("/projects/:id/uploads/:uid/chunks/:index", h.uploadChunk)
	g.Post("/projects/:id/uploads/:uid/complete", h.uploadComplete)
	g.Delete("/projects/:id/uploads/:uid", h.uploadAbort)
	g.Get("/projects/:id/exports", h.exports)
	g.Post("/projects/:id/exports", h.createExport)
	g.Get("/exports/:id", h.export)
	g.Post("/exports/:id/retry", h.retry)
	g.Get("/exports/:id/download", h.download)
}
func veHTTPError(c *fiber.Ctx, e error) error {
	if r, ok := insufficientPoints(c, e); ok {
		return r
	}
	status, code, message := 500, "INTERNAL_ERROR", "ระบบไม่สามารถดำเนินการได้ กรุณาลองใหม่"
	switch {
	case errors.Is(e, repo.ErrNotFound):
		status, code, message = 404, "NOT_FOUND", "ไม่พบข้อมูล"
	case errors.Is(e, repo.ErrVERevision):
		status, code, message = 409, "REVISION_CONFLICT", "โปรเจกต์มีการเปลี่ยนแปลง กรุณาโหลดข้อมูลล่าสุด"
	case errors.Is(e, repo.ErrVEIdempotency):
		status, code, message = 409, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ถูกใช้กับข้อมูลอื่นแล้ว"
	case errors.Is(e, repo.ErrVEUploadProgress):
		status, code, message = 409, "UPLOAD_IN_PROGRESS", "ไฟล์นี้กำลังประมวลผล กรุณารอสักครู่"
	case errors.Is(e, repo.ErrVEUploadLimit):
		status, code, message = 409, "UPLOAD_LIMIT_REACHED", "โปรเจกต์รองรับไฟล์ไม่เกิน 100 ไฟล์"
	case errors.Is(e, repo.ErrVEUploadBusy):
		status, code, message = 429, "UPLOAD_BUSY", "กำลังอัปโหลดหลายไฟล์ กรุณารอแล้วลองใหม่"
	case errors.Is(e, repo.ErrVEExportState):
		status, code, message = 409, "EXPORT_NOT_READY", "งานส่งออกยังไม่พร้อมใช้งาน"
	case errors.Is(e, f.ErrVEInvalid):
		status, code, message = 422, "VALIDATION_ERROR", "ข้อมูลตัดต่อไม่ถูกต้อง"
	case errors.Is(e, f.ErrVEMediaTooLong):
		status = 422
		code, message = f.VEUploadFailure(e)
	case errors.Is(e, f.ErrVEType):
		status, code, message = 422, "INVALID_FILE_TYPE", "ไม่รองรับหรือไม่สามารถอ่านไฟล์วิดีโอหรือเสียงนี้ได้"
	case errors.Is(e, f.ErrVEUploadSession):
		status, code, message = 404, "UPLOAD_SESSION_NOT_FOUND", "การอัปโหลดนี้หมดอายุหรือถูกยกเลิก กรุณาอัปโหลดใหม่"
	case errors.Is(e, f.ErrVESize):
		status, code, message = 413, "FILE_TOO_LARGE", "ไฟล์ใหญ่เกินที่ระบบรับได้ (วิดีโอและเสียงไม่เกิน 500 MB, รูปไม่เกิน 50 MB)"
	case errors.Is(e, f.ErrVESchema):
		status, code, message = 503, "MEDIA_UNAVAILABLE", "ระบบตัดต่อยังไม่พร้อมใช้งาน ฐานข้อมูลยังไม่ได้อัปเดต กรุณาติดต่อผู้ดูแลระบบ"
	case errors.Is(e, f.ErrVETextUnavailable):
		status, code, message = 503, "MEDIA_UNAVAILABLE", "ระบบยังไม่พร้อมฝังข้อความลงวิดีโอ กรุณาลบข้อความหรือติดต่อผู้ดูแลระบบ"
	case errors.Is(e, f.ErrVEVoiceUnavailable):
		status, code, message = 503, "VOICE_UNAVAILABLE", "ระบบสร้างเสียงยังไม่ได้ตั้งค่า"
	case errors.Is(e, f.ErrVEVoiceRejected):
		status, code, message = 422, "VOICE_REJECTED", "สร้างเสียงจากข้อความนี้ไม่ได้ กรุณาปรับข้อความหรือลักษณะเสียง"
	case errors.Is(e, f.ErrVEVoiceFailed):
		status, code, message = 502, "VOICE_FAILED", "สร้างเสียงไม่สำเร็จ กรุณาลองใหม่"
	case errors.Is(e, f.ErrVEMedia):
		status, code, message = 503, "MEDIA_UNAVAILABLE", "ระบบตัดต่อยังไม่พร้อมใช้งาน"
	case errors.Is(e, f.ErrVEStorage):
		status, code, message = 503, "STORAGE_UNAVAILABLE", "จัดเก็บไฟล์ไม่สำเร็จ กรุณาลองใหม่"
	}
	if status == 500 {
		// Unmapped: keep the cause in the server log (never in the response).
		rid, _ := c.Locals("requestID").(string)
		log.Printf("video_editor: %s %s unmapped error rid=%s: %v", c.Method(), c.Path(), rid, e)
	}
	return utils.Error(c, status, code, message, nil)
}
func veUser(c *fiber.Ctx) string { return c.Locals("userID").(string) }
func (h *VideoEditorHandler) list(c *fiber.Ctx) error {
	p, l := paginationParams(c)
	d, e := h.facade.Projects(c.Context(), veUser(c), p, l)
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *VideoEditorHandler) create(c *fiber.Ctx) error {
	var in req.CreateVideoEditorProject
	if c.BodyParser(&in) != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	d, created, e := h.facade.CreateProject(c.Context(), veUser(c), in)
	if e != nil {
		return veHTTPError(c, e)
	}
	status := 200
	if created {
		status = 201
	}
	return utils.Success(c, status, d)
}
func (h *VideoEditorHandler) detail(c *fiber.Ctx) error {
	d, e := h.facade.Project(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *VideoEditorHandler) remove(c *fiber.Ctx) error {
	id := c.Params("id")
	if e := h.facade.DeleteProject(c.Context(), id, veUser(c)); e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, fiber.Map{"id": id, "deleted": true})
}
func (h *VideoEditorHandler) save(c *fiber.Ctx) error {
	var in req.SaveVideoEditorProject
	if c.BodyParser(&in) != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	d, e := h.facade.SaveProject(c.Context(), c.Params("id"), veUser(c), in)
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *VideoEditorHandler) assets(c *fiber.Ctx) error {
	d, e := h.facade.Assets(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, dto.VideoEditorAssetsDTO{Items: d})
}
func (h *VideoEditorHandler) upload(c *fiber.Ctx) error {
	user := veUser(c)
	h.mu.Lock()
	if h.uploads[user] >= 2 {
		h.mu.Unlock()
		return veHTTPError(c, repo.ErrVEUploadBusy)
	}
	h.uploads[user]++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.uploads[user]--
		if h.uploads[user] == 0 {
			delete(h.uploads, user)
		}
		h.mu.Unlock()
	}()
	file, e := c.FormFile("file")
	if e != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	if file.Size > 50<<20 {
		return veHTTPError(c, f.ErrVESize)
	}
	reader, e := file.Open()
	if e != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	defer reader.Close()
	data, e := io.ReadAll(io.LimitReader(reader, (50<<20)+1))
	if e != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	if len(data) > 50<<20 {
		return veHTTPError(c, f.ErrVESize)
	}
	kind, _, e := mime.ParseMediaType(file.Header.Get("Content-Type"))
	if e != nil {
		return veHTTPError(c, f.ErrVEType)
	}
	d, created, e := h.facade.Upload(c.Context(), c.Params("id"), user, c.FormValue("clientUploadId"), file.Filename, kind, data)
	if e != nil {
		return veHTTPError(c, e)
	}
	status := 200
	if created {
		status = 201
	}
	return utils.Success(c, status, d)
}

// voice synthesizes speech into an audio asset; it shares the per-user upload slots with uploads.
func (h *VideoEditorHandler) voice(c *fiber.Ctx) error {
	user := veUser(c)
	h.mu.Lock()
	if h.uploads[user] >= 2 {
		h.mu.Unlock()
		return veHTTPError(c, repo.ErrVEUploadBusy)
	}
	h.uploads[user]++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.uploads[user]--
		if h.uploads[user] == 0 {
			delete(h.uploads, user)
		}
		h.mu.Unlock()
	}()
	var in f.VoiceRequest
	if e := c.BodyParser(&in); e != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	d, created, e := h.facade.GenerateVoice(c.Context(), c.Params("id"), user, in)
	if e != nil {
		return veHTTPError(c, e)
	}
	status := 200
	if created {
		status = 201
	}
	return utils.Success(c, status, d)
}

type veUploadStart struct {
	ClientUploadID string `json:"clientUploadId"`
	Name           string `json:"name"`
	MIME           string `json:"mime"`
	Size           int64  `json:"size"`
}

func (h *VideoEditorHandler) uploadStart(c *fiber.Ctx) error {
	var in veUploadStart
	if c.BodyParser(&in) != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	kind, _, e := mime.ParseMediaType(in.MIME)
	if e != nil {
		return veHTTPError(c, f.ErrVEType)
	}
	s, e := h.facade.StartUpload(c.Context(), c.Params("id"), veUser(c), in.ClientUploadID, in.Name, kind, in.Size)
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 201, fiber.Map{"uploadId": s.ID, "chunkSize": s.ChunkBytes(), "chunks": s.Chunks, "size": s.Size})
}
func (h *VideoEditorHandler) uploadChunk(c *fiber.Ctx) error {
	index, e := strconv.Atoi(c.Params("index"))
	if e != nil {
		return veHTTPError(c, f.ErrVEUploadChunk)
	}
	body := c.Body()
	received, e := h.facade.PutUploadChunk(c.Params("id"), veUser(c), c.Params("uid"), index, bytes.NewReader(body), int64(len(body)))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, fiber.Map{"received": received})
}
func (h *VideoEditorHandler) uploadComplete(c *fiber.Ctx) error {
	d, created, missing, e := h.facade.CompleteUpload(c.Context(), c.Params("id"), veUser(c), c.Params("uid"))
	if errors.Is(e, f.ErrVEUploadMissing) {
		return utils.Error(c, 409, "UPLOAD_INCOMPLETE", "ยังได้รับไฟล์ไม่ครบ กำลังส่งส่วนที่ขาด", fiber.Map{"missing": missing})
	}
	if e != nil {
		return veHTTPError(c, e)
	}
	status := 200
	if created {
		status = 201
	}
	return utils.Success(c, status, d)
}
func (h *VideoEditorHandler) uploadAbort(c *fiber.Ctx) error {
	h.facade.AbortUpload(c.Params("id"), veUser(c), c.Params("uid"))
	return utils.Success(c, 200, fiber.Map{"aborted": true})
}
func (h *VideoEditorHandler) exports(c *fiber.Ctx) error {
	d, e := h.facade.Exports(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, dto.VideoEditorExportsDTO{Items: d})
}
func (h *VideoEditorHandler) createExport(c *fiber.Ctx) error {
	var in req.CreateVideoEditorExport
	if c.BodyParser(&in) != nil {
		return veHTTPError(c, f.ErrVEInvalid)
	}
	d, created, e := h.facade.CreateExport(c.Context(), c.Params("id"), veUser(c), in)
	if e != nil {
		return veHTTPError(c, e)
	}
	status := 200
	if created {
		status = 202
	}
	return utils.Success(c, status, d)
}
func (h *VideoEditorHandler) export(c *fiber.Ctx) error {
	d, e := h.facade.Export(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *VideoEditorHandler) retry(c *fiber.Ctx) error {
	d, e := h.facade.RetryExport(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 202, d)
}
func (h *VideoEditorHandler) download(c *fiber.Ctx) error {
	d, e := h.facade.Download(c.Context(), c.Params("id"), veUser(c))
	if e != nil {
		return veHTTPError(c, e)
	}
	return utils.Success(c, 200, d)
}
