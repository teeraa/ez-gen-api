package handler_open_api

import (
	"errors"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"strconv"
	"strings"
	"time"
)

type DirectorHandler struct {
	facade *f.DirectorFacade
	rate   int
}

func NewDirectorHandler(facade *f.DirectorFacade, rate int) *DirectorHandler {
	if rate < 1 {
		rate = 10
	}
	return &DirectorHandler{facade, rate}
}
func (h *DirectorHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/director")
	g.Get("/capabilities", func(c *fiber.Ctx) error { return utils.Success(c, 200, h.facade.Capabilities()) })
	g.Get("/points", func(c *fiber.Ctx) error {
		v, e := h.facade.Points(c.Context(), productionUser(c))
		if e != nil {
			return directorHTTPError(c, e)
		}
		c.Set("Cache-Control", "private, no-store")
		return utils.Success(c, 200, v)
	})
	g.Get("/projects", func(c *fiber.Ctx) error {
		page, e := strconv.Atoi(c.Query("page", "1"))
		if e != nil {
			return directorHTTPError(c, &f.StoryFlowValidationError{Field: "page", Message: "เลขหน้าไม่ถูกต้อง"})
		}
		limit, e := strconv.Atoi(c.Query("limit", "20"))
		if e != nil {
			return directorHTTPError(c, &f.StoryFlowValidationError{Field: "limit", Message: "จำนวนรายการไม่ถูกต้อง"})
		}
		v, e := h.facade.List(c.Context(), productionUser(c), page, limit, c.Query("search"))
		if e != nil {
			return directorHTTPError(c, e)
		}
		return utils.Success(c, 200, v)
	})
	g.Get("/projects/:id", func(c *fiber.Ctx) error {
		v, etag, e := h.facade.Get(c.Context(), c.Params("id"), productionUser(c))
		if e != nil {
			return directorHTTPError(c, e)
		}
		c.Set("ETag", etag)
		c.Set("Cache-Control", "private, no-cache")
		if directorETagMatches(c.Get("If-None-Match"), etag) {
			return c.SendStatus(304)
		}
		return utils.Success(c, 200, v)
	})
	rate := limiter.New(limiter.Config{Next: func(c *fiber.Ctx) bool { return c.Method() != fiber.MethodPost }, Max: h.rate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return productionUser(c) }, LimitReached: func(c *fiber.Ctx) error {
		c.Set("Retry-After", "60")
		return utils.Error(c, 429, "RATE_LIMITED", "ส่งคำขอมากเกินไป กรุณารอสักครู่", nil)
	}})
	g.Post("/projects", rate, func(c *fiber.Ctx) error {
		var in req.DirectorRequest
		if e := storyFlowBody(c, &in); e != nil {
			return directorHTTPError(c, e)
		}
		v, e := h.facade.Create(c.Context(), productionUser(c), in)
		if e != nil {
			return directorHTTPError(c, e)
		}
		return utils.Success(c, 201, v)
	})
	for _, route := range []struct{ Method, Path, Action string }{
		{"PATCH", "/projects/:id", "project"}, {"DELETE", "/projects/:id", "delete"}, {"PUT", "/projects/:id/idea", "idea"},
		{"POST", "/projects/:id/cover", "cover-generate"}, {"PUT", "/projects/:id/cover", "cover-shot"}, {"DELETE", "/projects/:id/cover", "cover-delete"},
		{"POST", "/projects/:id/story", "story"}, {"PATCH", "/projects/:id/scenes/:target", "scene"}, {"POST", "/projects/:id/scenes/:target/rewrite", "rewrite"},
		{"PATCH", "/projects/:id/storyboard-settings", "settings"}, {"POST", "/projects/:id/storyboard", "storyboard"},
		{"PATCH", "/projects/:id/shots/:target", "shot"}, {"DELETE", "/projects/:id/shots/:target", "shot-delete"}, {"POST", "/projects/:id/shots/:target/regenerate", "regenerate"}, {"POST", "/projects/:id/shots/:target/retry", "retry"},
		{"POST", "/projects/:id/scenes/:target/shots", "add"}, {"POST", "/projects/:id/video", "video"},
	} {
		g.Add(route.Method, route.Path, rate, func(c *fiber.Ctx) error {
			var in req.DirectorRequest
			if len(c.Body()) > 0 {
				if e := storyFlowBody(c, &in); e != nil {
					return directorHTTPError(c, e)
				}
			}
			if route.Action == "delete" && in.Revision == nil {
				p, _, e := h.facade.Get(c.Context(), c.Params("id"), productionUser(c))
				if e != nil {
					return directorHTTPError(c, e)
				}
				in.Revision = &p.Revision
			}
			if in.Revision == nil && c.Query("revision") != "" {
				n, e := strconv.Atoi(c.Query("revision"))
				if e != nil {
					return directorHTTPError(c, &f.StoryFlowValidationError{Field: "revision", Message: "รุ่นข้อมูลไม่ถูกต้อง"})
				}
				in.Revision = &n
			}
			v, e := h.facade.Mutate(c.Context(), c.Params("id"), productionUser(c), route.Action, c.Params("target"), in)
			if e != nil {
				return directorHTTPError(c, e)
			}
			if route.Action == "delete" {
				return c.SendStatus(204)
			}
			return utils.Success(c, 200, v)
		})
	}
}
func directorETagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		s := strings.TrimSpace(candidate)
		if s == "*" || strings.TrimPrefix(s, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}
func directorHTTPError(c *fiber.Ctx, e error) error {
	var d *f.DirectorError
	var v *f.StoryFlowValidationError
	var points *f.DirectorInsufficientPoints
	switch {
	case errors.As(e, &d):
		if d.CurrentRevision != nil {
			return utils.Error(c, d.Status, d.Code, d.Message, struct {
				CurrentRevision int `json:"currentRevision"`
			}{*d.CurrentRevision})
		}
		return utils.Error(c, d.Status, d.Code, d.Message, nil)
	case errors.As(e, &v):
		return utils.Error(c, 400, "VALIDATION_ERROR", v.Message, []struct {
			Field   string `json:"field"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		}{{v.Field, "invalid", v.Message}})
	case errors.As(e, &points):
		return utils.Error(c, 402, "INSUFFICIENT_POINTS", "พอยต์ไม่เพียงพอ กรุณาเติมพอยต์ก่อนลองใหม่", struct {
			Required int   `json:"required"`
			Balance  int64 `json:"balance"`
		}{points.Required, points.Balance})
	case errors.Is(e, f.ErrDirectorIdempotencyConflict):
		return utils.Error(c, 409, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ถูกใช้กับข้อมูลอื่นแล้ว", nil)
	case errors.Is(e, f.ErrDirectorProjectNotFound):
		return utils.Error(c, 404, "PROJECT_NOT_FOUND", "ไม่พบโปรเจกต์นี้", nil)
	case errors.Is(e, f.ErrDirectorAdmissionLimit):
		c.Set("Retry-After", "2")
		return utils.Error(c, 429, "RATE_LIMITED", "มีงานพร้อมกันถึงขีดจำกัดแล้ว กรุณารอสักครู่", nil)
	default:
		return utils.Error(c, 500, "INTERNAL_ERROR", "เกิดข้อผิดพลาด กรุณาลองใหม่พร้อมแจ้งรหัสคำขอ", nil)
	}
}
