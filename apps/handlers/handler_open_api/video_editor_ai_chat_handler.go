package handler_open_api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type VideoEditorAIChatHandler struct{ facade *f.VideoEditorAIChatFacade }

func NewVideoEditorAIChatHandler(facade *f.VideoEditorAIChatFacade) *VideoEditorAIChatHandler {
	return &VideoEditorAIChatHandler{facade}
}

func (h *VideoEditorAIChatHandler) RegisterRoutes(r fiber.Router) {
	// The signed-in user's own chat token usage and estimated cost (hidden page /studio/ai-usage).
	r.Get("/video-editor/ai-chat/usage", func(c *fiber.Ctx) error {
		user, ok := c.Locals("userID").(string)
		if !ok {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		if _, e := uuid.Parse(user); e != nil {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		d, e := h.facade.Usage(c.Context(), user, c.QueryInt("days", 30))
		if e != nil {
			log.Printf("video_editor_ai_chat: usage: %v", e)
			return utils.Error(c, 500, "USAGE_FAILED", "โหลดข้อมูลการใช้งานไม่สำเร็จ", nil)
		}
		return utils.Success(c, 200, d)
	})
	g := r.Group("/video-editor/projects/:id/ai-chat", func(c *fiber.Ctx) error {
		user, ok := c.Locals("userID").(string)
		if !ok {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		if _, e := uuid.Parse(user); e != nil {
			return utils.Error(c, 401, "UNAUTHORIZED", "กรุณาเข้าสู่ระบบ", nil)
		}
		for _, name := range []string{"id", "mid"} {
			if v := c.Params(name); v != "" {
				if _, e := uuid.Parse(v); e != nil {
					return veHTTPError(c, repo.ErrNotFound)
				}
			}
		}
		if len(c.Body()) > 64<<10 {
			return chatHTTPError(c, f.ErrAIChatInput)
		}
		return c.Next()
	})
	g.Get("", func(c *fiber.Ctx) error {
		d, e := h.facade.Thread(c.Context(), c.Params("id"), veUser(c))
		if e != nil {
			return chatHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	// Clears the thread; the web calls it when a project is opened so each visit starts fresh.
	g.Delete("", func(c *fiber.Ctx) error {
		d, e := h.facade.Clear(c.Context(), c.Params("id"), veUser(c))
		if e != nil {
			return chatHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	// The reply is a text/event-stream: status events while tools run, then one message
	// event (the stored reply) or an error event.
	g.Post("/messages", func(c *fiber.Ctx) error {
		var in req.SendVideoEditorAIChat
		if c.BodyParser(&in) != nil {
			return chatHTTPError(c, f.ErrAIChatInput)
		}
		project, user := c.Params("id"), veUser(c)
		if e := h.facade.Precheck(c.Context(), project, user, in); e != nil {
			return chatHTTPError(c, e)
		}
		c.Set("Content-Type", "text/event-stream; charset=utf-8")
		c.Set("Cache-Control", "no-cache")
		c.Set("X-Accel-Buffering", "no")
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			ctx, cancel := context.WithTimeout(context.Background(), f.AIChatTimeout())
			defer cancel()
			emit := func(event dto.VideoEditorAIChatEventDTO) {
				raw, _ := json.Marshal(event)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, raw)
				if w.Flush() != nil {
					cancel() // the client left; the reply is still stored
				}
			}
			emit(dto.VideoEditorAIChatEventDTO{Type: "status", Tool: "thinking", Label: "กำลังคิด"})
			if e := h.facade.Send(ctx, project, user, in, emit); e != nil {
				log.Printf("video_editor_ai_chat: send: %v", e)
				code, message := chatErrorBody(e)
				emit(dto.VideoEditorAIChatEventDTO{Type: "error", Code: code, Text: message})
			}
		})
		return nil
	})
	g.Post("/messages/:mid/apply", func(c *fiber.Ctx) error {
		d, e := h.facade.Apply(c.Context(), c.Params("id"), c.Params("mid"), veUser(c))
		if e != nil {
			return chatHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
	g.Post("/messages/:mid/discard", func(c *fiber.Ctx) error {
		d, e := h.facade.Discard(c.Context(), c.Params("id"), c.Params("mid"), veUser(c))
		if e != nil {
			return chatHTTPError(c, e)
		}
		return utils.Success(c, 200, d)
	})
}

func chatErrorBody(e error) (string, string) {
	if _, ok := f.IsInsufficientPoints(e); ok {
		return "INSUFFICIENT_POINTS", "พอยต์ไม่เพียงพอ กรุณาเติมพอยต์ก่อนลองใหม่"
	}
	switch {
	case errors.Is(e, f.ErrAIChatBusy):
		return "AI_CHAT_BUSY", "AI กำลังตอบข้อความก่อนหน้าอยู่ กรุณารอสักครู่"
	case errors.Is(e, repo.ErrAIDaily):
		return "AI_DAILY_LIMIT", "ใช้ AI ครบโควตาวันนี้แล้ว"
	}
	return "AI_CHAT_FAILED", "AI ตอบไม่สำเร็จ กรุณาลองใหม่"
}

func chatHTTPError(c *fiber.Ctx, e error) error {
	if r, ok := insufficientPoints(c, e); ok {
		return r
	}
	switch {
	case errors.Is(e, f.ErrAIChatInput):
		return utils.Error(c, 400, "VALIDATION_ERROR", "ข้อความต้องมี 1–2000 ตัวอักษร", nil)
	case errors.Is(e, f.ErrAIChatModel):
		return utils.Error(c, 400, "AI_MODEL_UNAVAILABLE", "รุ่น AI นี้ใช้ไม่ได้ กรุณาเลือกรุ่นอื่น", nil)
	case errors.Is(e, f.ErrAIChatConsent):
		return utils.Error(c, 422, "AI_CONSENT_REQUIRED", "กรุณายอมรับการส่งข้อมูลไปยังผู้ให้บริการ AI ก่อน", nil)
	case errors.Is(e, f.ErrAIChatProposal):
		return utils.Error(c, 409, "AI_PROPOSAL_NOT_PENDING", "ข้อเสนอนี้ถูกใช้ ยกเลิก หรือหมดอายุแล้ว", nil)
	case errors.Is(e, f.ErrAIChatBusy):
		return utils.Error(c, 429, "AI_CHAT_BUSY", "AI กำลังตอบข้อความก่อนหน้าอยู่ กรุณารอสักครู่", nil)
	case errors.Is(e, repo.ErrVERevision):
		return utils.Error(c, 409, "REVISION_CONFLICT", "timeline เปลี่ยนไปหลัง AI เสนอการแก้ กรุณาขอให้ AI ทำใหม่", nil)
	}
	return aiHTTPError(c, e)
}
