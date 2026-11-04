package handler_open_api

import (
	"errors"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

type StudioHandler struct {
	facade        *f.StudioFacade
	jobsPerMinute int
}

func NewStudioHandler(facade *f.StudioFacade, jobsPerMinute int) *StudioHandler {
	if jobsPerMinute < 1 {
		jobsPerMinute = 10
	}
	return &StudioHandler{facade: facade, jobsPerMinute: jobsPerMinute}
}
func (h *StudioHandler) RegisterAuth(r fiber.Router) {
	rate := limiter.New(limiter.Config{Max: 10, Expiration: time.Minute, LimitReached: func(c *fiber.Ctx) error {
		return utils.Error(c, 429, "RATE_LIMITED", "เข้าสู่ระบบบ่อยเกินไป กรุณารอสักครู่แล้วลองอีกครั้ง", nil)
	}})
	r.Post("/auth/register", rate, h.register)
	r.Post("/auth/login", rate, h.login)
}
func (h *StudioHandler) RegisterRoutes(r fiber.Router) {
	r.Get("/auth/me", h.me)
	r.Get("/image-capabilities", h.capabilities)
	r.Post("/image-jobs", limiter.New(limiter.Config{Max: h.jobsPerMinute, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return c.Locals("userID").(string) }, LimitReached: func(c *fiber.Ctx) error {
		return utils.Error(c, 429, "RATE_LIMITED", "ส่งคำขอสร้างภาพบ่อยเกินไป กรุณารอหนึ่งนาทีแล้วลองอีกครั้ง", nil)
	}}), h.image)
	r.Post("/assets", h.createAsset)
	r.Get("/assets", h.listAssets)
	r.Get("/assets/:id", h.asset)
	r.Put("/assets/:id", h.updateAsset)
	r.Delete("/assets/:id", h.deleteAsset)
}
func studioError(c *fiber.Ctx, e error) error {
	switch {
	case errors.Is(e, f.ErrUnauthorized):
		return utils.Error(c, 401, "UNAUTHORIZED", "อีเมลหรือรหัสผ่านไม่ถูกต้อง", nil)
	case errors.Is(e, f.ErrNotFound):
		return utils.Error(c, 404, "NOT_FOUND", "ไม่พบข้อมูลที่ต้องการ", nil)
	case errors.Is(e, f.ErrConflict):
		return utils.Error(c, 409, "CONFLICT", "มีข้อมูลนี้อยู่แล้ว", nil)
	case errors.Is(e, f.ErrValidation):
		return utils.Error(c, 400, "VALIDATION_ERROR", "ข้อมูลไม่ถูกต้อง กรุณาตรวจสอบอีกครั้ง", nil)
	case errors.Is(e, f.ErrAssetSource):
		return utils.Error(c, 409, "ASSET_SOURCE_NOT_READY", "กรุณาเลือกภาพที่สร้างเสร็จแล้วหรือภาพอัปโหลดที่ยังใช้งานได้", nil)
	case errors.Is(e, f.ErrCharacterReferenceUnavailable):
		return utils.Error(c, 503, "CHARACTER_REFERENCE_UNAVAILABLE", "ยังไม่พร้อมวิเคราะห์ภาพอ้างอิงตัวละคร กรุณาติดต่อผู้ดูแลระบบหรือลองสร้างจากข้อความอย่างเดียว", nil)
	case errors.Is(e, f.ErrImageUnavailable):
		return utils.Error(c, 503, "IMAGE_UNAVAILABLE", "กรุณาติดต่อผู้ดูแลระบบเพื่อตั้งค่าบริการสร้างภาพและเปิดใช้งานโมเดล", nil)
	case errors.Is(e, f.ErrIdempotencyMismatch):
		return utils.Error(c, 409, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ใช้กับข้อมูลอื่นไปแล้ว กรุณาส่งคำขอใหม่", nil)
	case errors.Is(e, f.ErrRateLimit):
		return utils.Error(c, 429, "RATE_LIMITED", "กรุณารอให้งานสร้างภาพที่กำลังทำอยู่เสร็จก่อน", nil)
	case errors.Is(e, f.ErrImageExpired):
		return utils.Error(c, 422, "IMAGE_EXPIRED", "กรุณาอัปโหลดภาพอ้างอิงใหม่", nil)
	default:
		return utils.Error(c, 500, "INTERNAL_ERROR", "ดำเนินการไม่สำเร็จ กรุณาลองอีกครั้ง", nil)
	}
}
func parseStudio(c *fiber.Ctx, out any) error {
	if e := c.BodyParser(out); e != nil {
		return e
	}
	if errs := validateRequest(out); errs != nil {
		return errors.New("validation")
	}
	return nil
}
func (h *StudioHandler) auth(c *fiber.Ctx, register bool) error {
	var r req.AuthRequest
	if parseStudio(c, &r) != nil || (len([]byte(r.Password)) > 72 || len([]byte(r.Password)) < 10) {
		return utils.Error(c, 400, "VALIDATION_ERROR", "กรุณากรอกอีเมลให้ถูกต้อง และใช้รหัสผ่านอย่างน้อย 10 ตัวอักษร โดยมีขนาดไม่เกิน 72 ไบต์", nil)
	}
	d, e := h.facade.Authenticate(c.Context(), r, register)
	if e != nil {
		return studioError(c, e)
	}
	status := 200
	if register {
		status = 201
	}
	c.Set("Cache-Control", "no-store")
	return utils.Success(c, status, d)
}
func (h *StudioHandler) register(c *fiber.Ctx) error { return h.auth(c, true) }
func (h *StudioHandler) login(c *fiber.Ctx) error    { return h.auth(c, false) }
func (h *StudioHandler) me(c *fiber.Ctx) error {
	d, e := h.facade.Me(c.Context(), c.Locals("userID").(string))
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *StudioHandler) capabilities(c *fiber.Ctx) error {
	return utils.Success(c, 200, h.facade.Capabilities())
}
func (h *StudioHandler) image(c *fiber.Ctx) error {
	var r req.CreateImageRequest
	if parseStudio(c, &r) != nil {
		return utils.Error(c, 400, "VALIDATION_ERROR", "กรุณากรอกคำอธิบายภาพ เลือกสัดส่วนที่รองรับ และส่งรหัสคำขอให้ครบ", nil)
	}
	d, e := h.facade.CreateImage(c.Context(), c.Locals("userID").(string), r)
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 201, d)
}
func (h *StudioHandler) createAsset(c *fiber.Ctx) error {
	var r req.CreateAssetRequest
	if parseStudio(c, &r) != nil {
		return utils.Error(c, 400, "VALIDATION_ERROR", "ข้อมูลชิ้นงานไม่ถูกต้อง กรุณาตรวจสอบอีกครั้ง", nil)
	}
	d, e := h.facade.CreateAsset(c.Context(), c.Locals("userID").(string), r)
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 201, d)
}
func validStudioID(c *fiber.Ctx) bool { _, e := uuid.Parse(c.Params("id")); return e == nil }
func (h *StudioHandler) asset(c *fiber.Ctx) error {
	if !validStudioID(c) {
		return utils.Error(c, 400, "VALIDATION_ERROR", "รหัสชิ้นงานไม่ถูกต้อง", nil)
	}
	d, e := h.facade.GetAsset(c.Context(), c.Params("id"), c.Locals("userID").(string))
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *StudioHandler) listAssets(c *fiber.Ctx) error {
	if utf8.RuneCountInString(c.Query("search")) > 200 {
		return utils.Error(c, 400, "VALIDATION_ERROR", "คำค้นหายาวเกินไป กรุณาใช้ไม่เกิน 200 ตัวอักษร", nil)
	}
	page, limit := paginationParams(c)
	d, e := h.facade.ListAssets(c.Context(), c.Locals("userID").(string), c.Query("type"), strings.TrimSpace(c.Query("search")), page, limit)
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *StudioHandler) updateAsset(c *fiber.Ctx) error {
	if !validStudioID(c) {
		return utils.Error(c, 400, "VALIDATION_ERROR", "รหัสชิ้นงานไม่ถูกต้อง", nil)
	}
	var r req.UpdateAssetRequest
	if parseStudio(c, &r) != nil {
		return utils.Error(c, 400, "VALIDATION_ERROR", "ข้อมูลชิ้นงานไม่ถูกต้อง กรุณาตรวจสอบอีกครั้ง", nil)
	}
	d, e := h.facade.UpdateAsset(c.Context(), c.Params("id"), c.Locals("userID").(string), r)
	if e != nil {
		return studioError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *StudioHandler) deleteAsset(c *fiber.Ctx) error {
	if !validStudioID(c) {
		return utils.Error(c, 400, "VALIDATION_ERROR", "รหัสชิ้นงานไม่ถูกต้อง", nil)
	}
	if e := h.facade.DeleteAsset(c.Context(), c.Params("id"), c.Locals("userID").(string)); e != nil {
		return studioError(c, e)
	}
	return c.SendStatus(204)
}
