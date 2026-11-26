package handler_open_api

import (
	"errors"
	"io"
	"time"

	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// FilmHandler serves the ภาพยนตร์ menu under /film. It is separate from DirectorHandler.
type FilmHandler struct {
	facade *f.FilmFacade
	rate   int
}

func NewFilmHandler(facade *f.FilmFacade, rate int) *FilmHandler {
	if rate < 1 {
		rate = 10
	}
	return &FilmHandler{facade, rate}
}

func filmHTTPError(c *fiber.Ctx, e error) error {
	switch {
	case errors.Is(e, f.ErrFilmNotFound):
		return utils.Error(c, 404, "FILM_NOT_FOUND", "ไม่พบข้อมูลนี้", nil)
	case errors.Is(e, f.ErrFilmIdempotency):
		return utils.Error(c, 409, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ถูกใช้กับข้อมูลอื่นแล้ว", nil)
	case errors.Is(e, f.ErrFilmDuplicate):
		return utils.Error(c, 409, "ELEMENT_NAME_TAKEN", "มีองค์ประกอบชื่อนี้ในเรื่องแล้ว", nil)
	case errors.Is(e, f.ErrFilmAdmission):
		c.Set("Retry-After", "5")
		return utils.Error(c, 429, "RATE_LIMITED", "มีงานที่รออยู่มากเกินไป กรุณารอสักครู่", nil)
	}
	// Validation, insufficient points and DirectorError share the Director response shapes.
	return directorHTTPError(c, e)
}

func (h *FilmHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/film")
	rate := limiter.New(limiter.Config{Next: func(c *fiber.Ctx) bool { return c.Method() != fiber.MethodPost }, Max: h.rate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return "film:" + productionUser(c) }, LimitReached: func(c *fiber.Ctx) error {
		c.Set("Retry-After", "60")
		return utils.Error(c, 429, "RATE_LIMITED", "ส่งคำขอมากเกินไป กรุณารอสักครู่", nil)
	}})
	respond := func(c *fiber.Ctx, status int, v any, e error) error {
		if e != nil {
			return filmHTTPError(c, e)
		}
		c.Set("Cache-Control", "private, no-cache")
		return utils.Success(c, status, v)
	}
	g.Get("/series/:id/elements", func(c *fiber.Ctx) error {
		v, e := h.facade.ListElements(c.Context(), c.Params("id"), productionUser(c))
		return respond(c, 200, v, e)
	})
	g.Post("/series/:id/elements", rate, func(c *fiber.Ctx) error {
		var in req.FilmElementRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.CreateElement(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 201, v, e)
	})
	g.Patch("/elements/:id", func(c *fiber.Ctx) error {
		var in req.FilmElementRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.UpdateElement(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 200, v, e)
	})
	g.Delete("/elements/:id", func(c *fiber.Ctx) error {
		if e := h.facade.DeleteElement(c.Context(), c.Params("id"), productionUser(c)); e != nil {
			return filmHTTPError(c, e)
		}
		return c.SendStatus(204)
	})
	g.Post("/elements/:id/image", rate, func(c *fiber.Ctx) error {
		var in req.FilmElementImageRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.RequestElementImage(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 202, v, e)
	})
	g.Get("/episodes/:id", func(c *fiber.Ctx) error {
		v, e := h.facade.GetEpisode(c.Context(), c.Params("id"), productionUser(c))
		return respond(c, 200, v, e)
	})
	g.Put("/episodes/:id/shots", func(c *fiber.Ctx) error {
		var in req.FilmShotsRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.SaveShots(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 200, v, e)
	})
	g.Post("/episodes/:id/export", rate, func(c *fiber.Ctx) error {
		var in req.FilmExportRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.RequestExport(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 202, v, e)
	})
	g.Post("/episodes/:id/music", rate, func(c *fiber.Ctx) error {
		file, e := c.FormFile("file")
		if e != nil {
			return filmHTTPError(c, &f.DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: "กรุณาเลือกไฟล์เพลง"})
		}
		if file.Size > f.FilmMusicMaxBytes {
			return utils.Error(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "ไฟล์เพลงใหญ่เกิน 30 MB", nil)
		}
		reader, e := file.Open()
		if e != nil {
			return filmHTTPError(c, &f.DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: "อ่านไฟล์เพลงไม่ได้"})
		}
		defer reader.Close()
		data, e := io.ReadAll(io.LimitReader(reader, f.FilmMusicMaxBytes+1))
		if e != nil {
			return filmHTTPError(c, &f.DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: "อ่านไฟล์เพลงไม่ได้"})
		}
		v, e := h.facade.SetMusic(c.Context(), c.Params("id"), productionUser(c), c.FormValue("idempotencyKey"), file.Filename, data)
		return respond(c, 200, v, e)
	})
	g.Patch("/episodes/:id/music", func(c *fiber.Ctx) error {
		var in req.FilmMusicRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.UpdateMusic(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 200, v, e)
	})
	g.Put("/episodes/:id/skills", func(c *fiber.Ctx) error {
		var in req.FilmSkillsRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.SetSkills(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 200, v, e)
	})
	g.Post("/episodes/:id/redub", rate, func(c *fiber.Ctx) error {
		var in req.FilmRedubRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.RequestRedub(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 202, v, e)
	})
	g.Post("/episodes/:id/plan", rate, func(c *fiber.Ctx) error {
		var in req.FilmPlanRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.RequestPlan(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 202, v, e)
	})
	g.Post("/episodes/:id/plan/confirm", rate, func(c *fiber.Ctx) error {
		var in req.FilmPlanConfirmRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.ConfirmPlan(c.Context(), c.Params("id"), productionUser(c), in)
		return respond(c, 200, v, e)
	})
	g.Post("/episodes/:id/shots/:shot/video", rate, func(c *fiber.Ctx) error {
		var in req.FilmVideoRequest
		if e := storyFlowBody(c, &in); e != nil {
			return filmHTTPError(c, e)
		}
		v, e := h.facade.RequestVideo(c.Context(), c.Params("id"), c.Params("shot"), productionUser(c), in)
		return respond(c, 202, v, e)
	})
}
