package handler_open_api

import (
	"bytes"
	"encoding/json"
	"errors"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"io"
	"time"
)

type StoryFlowHandler struct {
	facade *f.StoryFlowFacade
	rate   int
}

func NewStoryFlowHandler(facade *f.StoryFlowFacade, rate int) *StoryFlowHandler {
	if rate < 1 {
		rate = 10
	}
	return &StoryFlowHandler{facade, rate}
}
func (h *StoryFlowHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/story-flow")
	g.Get("/capabilities", func(c *fiber.Ctx) error { return utils.Success(c, 200, h.facade.Capabilities()) })
	g.Get("/projects", func(c *fiber.Ctx) error {
		p, l := paginationParams(c)
		v, e := h.facade.List(c.Context(), productionUser(c), p, l)
		if e != nil {
			return storyFlowError(c, e)
		}
		return utils.Success(c, 200, v)
	})
	g.Get("/projects/:id", func(c *fiber.Ctx) error {
		v, e := h.facade.Get(c.Context(), c.Params("id"), productionUser(c))
		if e != nil {
			return storyFlowError(c, e)
		}
		return utils.Success(c, 200, v)
	})
	rate := limiter.New(limiter.Config{Max: h.rate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return productionUser(c) }, LimitReached: func(c *fiber.Ctx) error {
		return utils.Error(c, 429, "RATE_LIMITED", "ส่งคำขอมากเกินไป กรุณารอสักครู่", nil)
	}})
	g.Post("/projects", rate, func(c *fiber.Ctx) error {
		var in req.CreateStoryFlowRequest
		if e := storyFlowBody(c, &in); e != nil {
			return storyFlowError(c, e)
		}
		v, e := h.facade.Create(c.Context(), productionUser(c), in)
		if e != nil {
			return storyFlowError(c, e)
		}
		return utils.Success(c, 201, v)
	})
	for _, action := range []string{"story", "storyboard", "video", "retry"} {
		g.Post("/projects/:id/"+action, rate, func(c *fiber.Ctx) error {
			var in req.StoryFlowActionRequest
			if e := storyFlowBody(c, &in); e != nil {
				return storyFlowError(c, e)
			}
			v, e := h.facade.Action(c.Context(), c.Params("id"), productionUser(c), action, in)
			if e != nil {
				return storyFlowError(c, e)
			}
			return utils.Success(c, 200, v)
		})
	}
}
func storyFlowBody(c *fiber.Ctx, out any) error {
	d := json.NewDecoder(bytes.NewReader(c.Body()))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return &f.StoryFlowValidationError{Field: "request", Message: "รูปแบบข้อมูลไม่ถูกต้อง"}
	}
	var extra json.RawMessage
	if e := d.Decode(&extra); e != io.EOF {
		return &f.StoryFlowValidationError{Field: "request", Message: "รูปแบบข้อมูลไม่ถูกต้อง"}
	}
	return nil
}
func storyFlowError(c *fiber.Ctx, e error) error {
	var v *f.StoryFlowValidationError
	switch {
	case errors.As(e, &v):
		return utils.Error(c, 400, "VALIDATION_ERROR", v.Message, []struct {
			Field   string `json:"field"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		}{{v.Field, "invalid", v.Message}})
	case errors.Is(e, f.ErrIdempotencyMismatch):
		return utils.Error(c, 409, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ถูกใช้กับข้อมูลอื่นแล้ว", nil)
	case errors.Is(e, f.ErrNotFound), errors.Is(e, repo.ErrNotFound):
		return utils.Error(c, 404, "PROJECT_NOT_FOUND", "ไม่พบโปรเจกต์นี้", nil)
	case errors.Is(e, f.ErrStoryFlowUnavailable):
		return utils.Error(c, 503, "STORY_FLOW_UNAVAILABLE", "Story Flow ยังไม่พร้อมใช้งาน กรุณาติดต่อผู้ดูแลระบบ", nil)
	case errors.Is(e, f.ErrStoryFlowConflict), errors.Is(e, repo.ErrConflict):
		return utils.Error(c, 409, "STORY_FLOW_CONFLICT", "สถานะงานเปลี่ยนไป กรุณาโหลดข้อมูลล่าสุด", nil)
	default:
		return utils.Error(c, 500, "INTERNAL_ERROR", "เกิดข้อผิดพลาด กรุณาลองใหม่", nil)
	}
}
