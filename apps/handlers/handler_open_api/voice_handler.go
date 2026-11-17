package handler_open_api

import (
	"errors"
	"io"
	"sync"
	"time"

	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// VoiceHandler serves the Voice menu under /voice.
type VoiceHandler struct {
	facade   *f.VoiceFacade
	rate     int
	mu       sync.Mutex
	inFlight map[string]int
}

func NewVoiceHandler(facade *f.VoiceFacade, ratePerMinute int) *VoiceHandler {
	if ratePerMinute < 1 {
		ratePerMinute = 10
	}
	return &VoiceHandler{facade: facade, rate: ratePerMinute, inFlight: map[string]int{}}
}

// RegisterRoutes mounts on the authenticated /api/v1 router.
func (h *VoiceHandler) RegisterRoutes(r fiber.Router) {
	g := r.Group("/voice")
	// Each generation is a paid provider call, so it gets its own per-user limit.
	paid := limiter.New(limiter.Config{Max: h.rate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return "voice:" + userIDOf(c) }, LimitReached: func(c *fiber.Ctx) error {
		c.Set("Retry-After", "60")
		return utils.Error(c, fiber.StatusTooManyRequests, "RATE_LIMITED", "สร้างเสียงถี่เกินไป กรุณารอสักครู่", nil)
	}})
	g.Get("/capabilities", h.capabilities)
	g.Get("/generations", h.list)
	g.Post("/generations", paid, h.generate)
	g.Delete("/generations/:id", h.remove)
	g.Get("/profiles", h.profiles)
	g.Post("/profiles/design", h.createDesign)
	g.Post("/profiles/reference", h.createReference)
	g.Delete("/profiles/:id", h.removeProfile)
}

func voiceError(c *fiber.Ctx, e error) error {
	if r, ok := insufficientPoints(c, e); ok {
		return r
	}
	switch {
	case errors.Is(e, f.ErrValidation):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", "ข้อมูลไม่ถูกต้อง กรุณาตรวจข้อความ เสียง ความเร็ว และระดับเสียง", nil)
	case errors.Is(e, f.ErrConflict):
		return utils.Error(c, fiber.StatusConflict, "IDEMPOTENCY_CONFLICT", "รหัสคำขอนี้ถูกใช้กับข้อมูลอื่นแล้ว", nil)
	case errors.Is(e, f.ErrVoiceUnavailable):
		return utils.Error(c, fiber.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "ระบบสร้างเสียงยังไม่ได้ตั้งค่า กรุณาติดต่อผู้ดูแลระบบ", nil)
	case errors.Is(e, f.ErrVoiceNotGranted):
		return utils.Error(c, fiber.StatusServiceUnavailable, "VOICE_NOT_GRANTED", "บัญชี BytePlus ยังไม่ได้รับสิทธิ์ใช้บริการสร้างเสียง (seed-audio) หรือ API key ไม่ถูกต้อง กรุณาติดต่อผู้ดูแลระบบ", nil)
	case errors.Is(e, f.ErrVoiceRejected):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "VOICE_REJECTED", "สร้างเสียงจากข้อความนี้ไม่ได้ กรุณาปรับข้อความแล้วลองใหม่", nil)
	case errors.Is(e, f.ErrVoiceFailed):
		return utils.Error(c, fiber.StatusBadGateway, "VOICE_FAILED", "สร้างเสียงไม่สำเร็จ กรุณาลองใหม่", nil)
	case errors.Is(e, f.ErrVoiceProfileLimit):
		return utils.Error(c, fiber.StatusConflict, "VOICE_PROFILE_LIMIT", "สร้างเสียงของฉันได้สูงสุด 20 เสียง กรุณาลบเสียงที่ไม่ใช้ก่อน", nil)
	case errors.Is(e, f.ErrVoiceConsent):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "VOICE_CONSENT_REQUIRED", "กรุณายืนยันว่าเป็นเสียงของคุณ หรือได้รับอนุญาตจากเจ้าของเสียงแล้ว", nil)
	case errors.Is(e, f.ErrVoiceReferenceShort):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "VOICE_SAMPLE_TOO_SHORT", "คลิปเสียงสั้นเกินไป ต้องยาวอย่างน้อย 5 วินาที", nil)
	case errors.Is(e, f.ErrVoiceReferenceInvalid):
		return utils.Error(c, fiber.StatusUnprocessableEntity, "INVALID_FILE_TYPE", "อ่านไฟล์เสียงนี้ไม่ได้ กรุณาใช้ไฟล์ wav, mp3, m4a, ogg หรืออัดเสียงใหม่", nil)
	case errors.Is(e, f.ErrVoiceReferenceUnavailable):
		return utils.Error(c, fiber.StatusServiceUnavailable, "MEDIA_UNAVAILABLE", "เซิร์ฟเวอร์ยังไม่พร้อมประมวลผลไฟล์เสียง (ต้องติดตั้ง FFmpeg)", nil)
	case errors.Is(e, f.ErrVoiceStorage):
		return utils.Error(c, fiber.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "จัดเก็บไฟล์เสียงไม่สำเร็จ กรุณาลองใหม่", nil)
	}
	return mapFacadeError(c, e)
}

func (h *VoiceHandler) capabilities(c *fiber.Ctx) error {
	return utils.Success(c, fiber.StatusOK, h.facade.Capabilities())
}

func (h *VoiceHandler) list(c *fiber.Ctx) error {
	page, limit := paginationParams(c)
	d, e := h.facade.List(c.Context(), userIDOf(c), page, limit)
	if e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusOK, d)
}

func (h *VoiceHandler) generate(c *fiber.Ctx) error {
	user := userIDOf(c)
	// At most two syntheses per user at once; each holds a provider call of up to two minutes.
	h.mu.Lock()
	if h.inFlight[user] >= 2 {
		h.mu.Unlock()
		return utils.Error(c, fiber.StatusTooManyRequests, "VOICE_BUSY", "กำลังสร้างเสียงอยู่ กรุณารอให้เสร็จก่อน", nil)
	}
	h.inFlight[user]++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.inFlight[user]--; h.inFlight[user] <= 0 {
			delete(h.inFlight, user)
		}
		h.mu.Unlock()
	}()
	var in f.VoiceInput
	if e := c.BodyParser(&in); e != nil {
		return voiceError(c, f.ErrValidation)
	}
	d, created, e := h.facade.Generate(c.Context(), user, in)
	if e != nil {
		return voiceError(c, e)
	}
	status := fiber.StatusOK
	if created {
		status = fiber.StatusCreated
	}
	return utils.Success(c, status, d)
}

func (h *VoiceHandler) remove(c *fiber.Ctx) error {
	if e := h.facade.Delete(c.Context(), userIDOf(c), c.Params("id")); e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusOK, fiber.Map{"id": c.Params("id"), "deleted": true})
}

func (h *VoiceHandler) profiles(c *fiber.Ctx) error {
	items, e := h.facade.ListProfiles(c.Context(), userIDOf(c))
	if e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusOK, fiber.Map{"items": items})
}

type voiceDesignBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *VoiceHandler) createDesign(c *fiber.Ctx) error {
	var in voiceDesignBody
	if e := c.BodyParser(&in); e != nil {
		return voiceError(c, f.ErrValidation)
	}
	d, e := h.facade.CreateDesignProfile(c.Context(), userIDOf(c), in.Name, in.Description)
	if e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusCreated, d)
}

// createReference takes multipart fields name, consent ("true") and file (an upload or a browser recording).
func (h *VoiceHandler) createReference(c *fiber.Ctx) error {
	file, e := c.FormFile("file")
	if e != nil {
		return voiceError(c, f.ErrValidation)
	}
	// Recordings and uploads are converted to a <=30 s MP3, so allow some room over the 10 MB provider cap.
	const maxUpload = 3 * seedaudio.MaxReferenceBytes
	if file.Size > maxUpload {
		return utils.Error(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "ไฟล์เสียงใหญ่เกิน 30 MB", nil)
	}
	reader, e := file.Open()
	if e != nil {
		return voiceError(c, f.ErrValidation)
	}
	defer reader.Close()
	data, e := io.ReadAll(io.LimitReader(reader, maxUpload+1))
	if e != nil || len(data) > maxUpload {
		return voiceError(c, f.ErrVoiceReferenceInvalid)
	}
	d, e := h.facade.CreateReferenceProfile(c.Context(), userIDOf(c), c.FormValue("name"), c.FormValue("consent") == "true", data)
	if e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusCreated, d)
}

func (h *VoiceHandler) removeProfile(c *fiber.Ctx) error {
	if e := h.facade.DeleteProfile(c.Context(), userIDOf(c), c.Params("id")); e != nil {
		return voiceError(c, e)
	}
	return utils.Success(c, fiber.StatusOK, fiber.Map{"id": c.Params("id"), "deleted": true})
}
