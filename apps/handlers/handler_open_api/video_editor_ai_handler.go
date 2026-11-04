package handler_open_api

import (
	"errors"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type VideoEditorAIHandler struct{ facade *f.VideoEditorAIFacade }

func NewVideoEditorAIHandler(facade *f.VideoEditorAIFacade) *VideoEditorAIHandler {
	return &VideoEditorAIHandler{facade}
}
func (h *VideoEditorAIHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/video-editor", func(c *fiber.Ctx) error {
		user, ok := c.Locals("userID").(string)
		if !ok {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		if _, e := uuid.Parse(user); e != nil {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		if id := c.Params("id"); id != "" {
			if _, e := uuid.Parse(id); e != nil {
				return veHTTPError(c, repo.ErrNotFound)
			}
		}
		if len(c.Body()) > 256<<10 {
			return aiHTTPError(c, editorai.ErrTooLarge)
		}
		return c.Next()
	})
	g.Get("/ai-capabilities", func(c *fiber.Ctx) error { return utils.Success(c, 200, h.facade.Capabilities()) })
	g.Post("/projects/:id/ai-jobs", func(c *fiber.Ctx) error {
		var in req.CreateVideoEditorAIJob
		if c.BodyParser(&in) != nil {
			return veHTTPError(c, f.ErrVEInvalid)
		}
		d, created, e := h.facade.Create(c.Context(), c.Params("id"), veUser(c), in)
		if e != nil {
			return aiHTTPError(c, e)
		}
		status := 200
		if created {
			status = 202
		}
		return utils.Success(c, status, d)
	})
	g.Get("/projects/:id/ai-jobs", func(c *fiber.Ctx) error {
		d, e := h.facade.List(c.Context(), c.Params("id"), veUser(c))
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Get("/ai-jobs/:id", func(c *fiber.Ctx) error {
		d, e := h.facade.Get(c.Context(), c.Params("id"), veUser(c))
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Post("/ai-jobs/:id/select-concept", func(c *fiber.Ctx) error {
		var in req.SelectVideoEditorAIConcept
		if c.BodyParser(&in) != nil {
			return veHTTPError(c, f.ErrVEInvalid)
		}
		d, e := h.facade.Select(c.Context(), c.Params("id"), veUser(c), in.ConceptID)
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 202, d)
	})
	g.Put("/ai-jobs/:id/draft", func(c *fiber.Ctx) error {
		var in req.UpdateVideoEditorAIDraft
		if c.BodyParser(&in) != nil {
			return veHTTPError(c, f.ErrVEInvalid)
		}
		d, e := h.facade.UpdateDraft(c.Context(), c.Params("id"), veUser(c), in)
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Post("/ai-jobs/:id/apply", func(c *fiber.Ctx) error {
		var in req.ApplyVideoEditorAIDraft
		if c.BodyParser(&in) != nil {
			return veHTTPError(c, f.ErrVEInvalid)
		}
		d, e := h.facade.Apply(c.Context(), c.Params("id"), veUser(c), in)
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Post("/ai-jobs/:id/cancel", func(c *fiber.Ctx) error {
		d, e := h.facade.Cancel(c.Context(), c.Params("id"), veUser(c))
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Post("/ai-jobs/:id/retry", func(c *fiber.Ctx) error {
		var in req.RetryVideoEditorAIJob
		if c.BodyParser(&in) != nil {
			return veHTTPError(c, f.ErrVEInvalid)
		}
		d, e := h.facade.Retry(c.Context(), c.Params("id"), veUser(c), in.RequestID)
		if e != nil {
			return aiHTTPError(c, e)
		}
		return utils.Success(c, 202, d)
	})
}
func aiHTTPError(c *fiber.Ctx, e error) error {
	status, code, message := 0, "", ""
	switch {
	case errors.Is(e, repo.ErrAIBusy):
		status, code, message = 409, "AI_BUSY", "มีงานวิเคราะห์ที่ยังไม่เสร็จ กรุณารอหรือยกเลิกงานเดิม"
	case errors.Is(e, repo.ErrAIDaily):
		status, code, message = 429, "AI_DAILY_LIMIT", "ใช้จำนวนงานวิเคราะห์ครบสำหรับวันนี้แล้ว"
	case errors.Is(e, repo.ErrAISelection):
		status, code, message = 409, "AI_SELECTION_CONFLICT", "คอนเซปต์นี้ไม่พร้อมเลือก หรือได้เลือกเรื่องอื่นแล้ว"
	case errors.Is(e, repo.ErrAIDraft), errors.Is(e, repo.ErrAIState):
		status, code, message = 409, "AI_DRAFT_CONFLICT", "งานวิเคราะห์มีการเปลี่ยนแปลง กรุณาโหลดข้อมูลล่าสุด"
	case errors.Is(e, repo.ErrAIAlreadyApplied):
		status, code, message = 409, "AI_ALREADY_APPLIED", "นำฉบับร่างนี้ไปใช้แล้ว"
	case errors.Is(e, repo.ErrAIRetry):
		status, code, message = 409, "AI_RETRY_NOT_READY", "งานนี้ยังไม่พร้อมลองใหม่ หรือใช้จำนวนครั้งครบแล้ว"
	case errors.Is(e, editorai.ErrInvalid):
		status, code, message = 422, "AI_INVALID_RESULT", "ข้อมูลแผนตัดต่อไม่ถูกต้อง"
	case errors.Is(e, editorai.ErrTooLarge):
		status, code, message = 422, "AI_INPUT_TOO_LARGE", "ข้อมูลวิเคราะห์มีขนาดมากเกินไป"
	case errors.Is(e, editorai.ErrASR):
		status, code, message = 503, "ASR_UNAVAILABLE", "บริการถอดเสียงยังไม่พร้อมใช้งาน"
	case errors.Is(e, f.ErrAIStatusUnavailable):
		status, code, message = 503, "AI_STATUS_UNAVAILABLE", "อ่านสถานะงานไม่ได้ชั่วคราว กรุณาลองใหม่"
	case errors.Is(e, editorai.ErrUnavailable):
		status, code, message = 503, "AI_UNAVAILABLE", "ระบบวิเคราะห์ยังไม่พร้อมใช้งาน"
	default:
		return veHTTPError(c, e)
	}
	return utils.Error(c, status, code, message, nil)
}
