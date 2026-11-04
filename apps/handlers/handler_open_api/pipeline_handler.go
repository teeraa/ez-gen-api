package handler_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/google/uuid"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type pipelineAPI interface {
	Capabilities() dto.PipelineCapabilitiesDTO
	Create(context.Context, string, req.CreatePipelineRequest) (*dto.PipelineDTO, error)
	Get(context.Context, string, string) (*dto.PipelineDTO, error)
	List(context.Context, string, int, int) (*dto.PipelineListDTO, error)
	Retry(context.Context, string, string) (*dto.PipelineDTO, error)
	Finalize(context.Context, string, string, req.FinalizePipelineRequest) (*dto.PipelineDTO, error)
}
type PipelineHandler struct {
	facade pipelineAPI
	rate   int
}

func NewPipelineHandler(f pipelineAPI, rate int) *PipelineHandler {
	if rate < 1 {
		rate = 10
	}
	return &PipelineHandler{facade: f, rate: rate}
}
func (h *PipelineHandler) RegisterRoutes(r fiber.Router) {
	r.Use("/studio-pipelines", func(c *fiber.Ctx) error { c.Set("Cache-Control", "no-store"); return c.Next() })
	r.Get("/studio-pipeline-capabilities", func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")
		return utils.Success(c, 200, h.facade.Capabilities())
	})
	r.Get("/studio-pipelines", h.list)
	r.Get("/studio-pipelines/:id", h.get)
	rate := limiter.New(limiter.Config{Max: h.rate, Expiration: time.Minute, KeyGenerator: func(c *fiber.Ctx) string { return c.Locals("userID").(string) }, LimitReached: func(c *fiber.Ctx) error {
		return utils.Error(c, 429, "RATE_LIMITED", "ส่งคำขอบ่อยเกินไป กรุณารอสักครู่", nil)
	}})
	r.Post("/studio-pipelines/:id/retry", rate, h.retry)
	r.Post("/studio-pipelines/:id/finalize", rate, h.finalize)
	r.Post("/studio-pipelines", rate, h.create)
	if p, ok := h.facade.(pipelinePreflighter); ok {
		// Called while the user types (debounced), so it is not under the create rate limit;
		// it only measures text and never touches storage or the provider.
		r.Post("/studio-pipelines/preflight", func(c *fiber.Ctx) error {
			c.Set("Cache-Control", "no-store")
			if len(c.Body()) > 32<<10 {
				return utils.Error(c, 413, "FILE_TOO_LARGE", "คำขอยาวเกินไป", nil)
			}
			var in req.CreatePipelineRequest
			if err := json.Unmarshal(c.Body(), &in); err != nil {
				return utils.Error(c, 400, "VALIDATION_ERROR", "รูปแบบคำขอไม่ถูกต้อง", nil)
			}
			return utils.Success(c, 200, p.Preflight(in))
		})
	}
}

// pipelinePreflighter is optional so existing pipelineAPI fakes keep compiling.
type pipelinePreflighter interface {
	Preflight(req.CreatePipelineRequest) dto.PipelinePreflightDTO
}

type pipelineValidationDetail struct {
	Field   string `json:"field"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func pipelineError(c *fiber.Ctx, e error) error {
	if errors.Is(e, f.ErrDraftExpired) {
		return utils.Error(c, 409, "DRAFT_EXPIRED", "ฉบับร่างหมดอายุ กรุณาสร้างวิดีโอใหม่", nil)
	}
	var validation *f.PipelineValidationError
	if errors.As(e, &validation) {
		return utils.Error(c, 400, "VALIDATION_ERROR", validation.Message, []pipelineValidationDetail{{Field: validation.Field, Reason: validation.Reason, Message: validation.Message}})
	}
	if errors.Is(e, f.ErrModelInvalid) {
		return utils.Error(c, 422, "MODEL_NOT_SUPPORTED", "การตั้งค่าที่เลือกไม่รองรับภาพอ้างอิงหรือเสียงพูด", nil)
	}
	return studioError(c, e)
}

var pipelineFieldLabels = map[string]string{
	"referenceImageId": "ภาพอ้างอิง", "characterAssetId": "ตัวละครจากคลัง", "idempotencyKey": "รหัสคำขอ",
	"characterPrompt": "ลักษณะตัวละคร", "scenePrompt": "ฉากและการเคลื่อนไหว", "dialogue": "บทพูด",
	"visualStyle": "สไตล์ภาพ", "aspectRatio": "สัดส่วนวิดีโอ", "modelId": "โมเดล", "quality": "คุณภาพวิดีโอ",
	"durationSeconds": "ความยาววิดีโอ", "draft": "โหมดร่าง",
}

func pipelineInputDetail(field, reason string) pipelineValidationDetail {
	label, known := pipelineFieldLabels[field]
	if !known {
		return pipelineValidationDetail{Field: "body", Reason: "malformed_json", Message: "รูปแบบคำขอไม่ถูกต้อง กรุณาโหลดหน้าใหม่แล้วลองอีกครั้ง"}
	}
	message := "กรุณาตรวจสอบ" + label
	switch reason {
	case "required":
		message = "กรุณาระบุ" + label
	case "invalid_uuid":
		message = label + "ไม่ถูกต้อง กรุณาเลือกใหม่หรือเริ่มคำขอใหม่"
	case "too_long":
		message = label + "ยาวเกินขีดจำกัด กรุณาย่อแล้วลองอีกครั้ง"
	case "invalid_option":
		message = label + "ไม่รองรับ กรุณาเลือกค่าที่รองรับ"
	case "out_of_range":
		message = "ความยาววิดีโอต้องอยู่ระหว่าง 4 ถึง 15 วินาที"
	case "invalid_type":
		message = "ชนิดข้อมูลของ" + label + "ไม่ถูกต้อง กรุณาตรวจสอบแล้วลองอีกครั้ง"
	}
	return pipelineValidationDetail{Field: field, Reason: reason, Message: message}
}
func parsePipelineCreate(c *fiber.Ctx, in *req.CreatePipelineRequest) []pipelineValidationDetail {
	malformed := func() []pipelineValidationDetail {
		return []pipelineValidationDetail{pipelineInputDetail("body", "malformed_json")}
	}
	body := bytes.TrimSpace(c.Body())
	if len(body) == 0 || body[0] != '{' {
		return malformed()
	}
	if err := c.BodyParser(in); err != nil {
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			if _, known := pipelineFieldLabels[typeError.Field]; known {
				return []pipelineValidationDetail{pipelineInputDetail(typeError.Field, "invalid_type")}
			}
		}
		return malformed()
	}
	err := validate.Struct(in)
	if err == nil {
		return nil
	}
	var fields validator.ValidationErrors
	if !errors.As(err, &fields) {
		return malformed()
	}
	details := make([]pipelineValidationDetail, 0, len(fields))
	requestType := reflect.TypeOf(*in)
	for _, fe := range fields {
		structField, ok := requestType.FieldByName(fe.StructField())
		if !ok {
			continue
		}
		field := strings.Split(structField.Tag.Get("json"), ",")[0]
		reason := "invalid_option"
		switch fe.Tag() {
		case "required":
			reason = "required"
		case "uuid", "uuid4":
			reason = "invalid_uuid"
		case "max":
			if field == "durationSeconds" {
				reason = "out_of_range"
			} else {
				reason = "too_long"
			}
		case "min":
			if field == "durationSeconds" {
				reason = "out_of_range"
			} else {
				reason = "required"
			}
		}
		details = append(details, pipelineInputDetail(field, reason))
		if len(details) == 8 {
			break
		}
	}
	if len(details) == 0 {
		return malformed()
	}
	return details
}

func (h *PipelineHandler) create(c *fiber.Ctx) error {
	var in req.CreatePipelineRequest
	if details := parsePipelineCreate(c, &in); len(details) > 0 {
		return utils.Error(c, 400, "VALIDATION_ERROR", details[0].Message, details)
	}
	d, e := h.facade.Create(c.Context(), c.Locals("userID").(string), in)
	if e != nil {
		return pipelineError(c, e)
	}
	return utils.Success(c, 201, d)
}
func (h *PipelineHandler) get(c *fiber.Ctx) error {
	if _, e := uuid.Parse(c.Params("id")); e != nil {
		return utils.Error(c, 404, "NOT_FOUND", "ไม่พบงานที่ต้องการ", nil)
	}
	d, e := h.facade.Get(c.Context(), c.Params("id"), c.Locals("userID").(string))
	if e != nil {
		return pipelineError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *PipelineHandler) retry(c *fiber.Ctx) error {
	if _, e := uuid.Parse(c.Params("id")); e != nil {
		return utils.Error(c, 404, "NOT_FOUND", "ไม่พบงานที่ต้องการ", nil)
	}
	var body map[string]json.RawMessage
	if len(c.Body()) > 0 && (json.Unmarshal(c.Body(), &body) != nil || body == nil || len(body) > 0) {
		return utils.Error(c, 400, "VALIDATION_ERROR", "ไม่รองรับข้อมูลเพิ่มเติมในการดำเนินการต่อ", nil)
	}
	d, e := h.facade.Retry(c.Context(), c.Params("id"), c.Locals("userID").(string))
	if e != nil {
		return pipelineError(c, e)
	}
	return utils.Success(c, 200, d)
}
func (h *PipelineHandler) list(c *fiber.Ctx) error {
	page, limit := 1, 20
	var e error
	if c.Query("page") != "" {
		page, e = strconv.Atoi(c.Query("page"))
		if e != nil || page < 1 {
			return utils.Error(c, 400, "VALIDATION_ERROR", "หน้าที่เลือกไม่ถูกต้อง", nil)
		}
	}
	if c.Query("limit") != "" {
		limit, e = strconv.Atoi(c.Query("limit"))
		if e != nil || limit < 1 || limit > 50 {
			return utils.Error(c, 400, "VALIDATION_ERROR", "จำนวนรายการไม่ถูกต้อง", nil)
		}
	}
	d, e := h.facade.List(c.Context(), c.Locals("userID").(string), page, limit)
	if e != nil {
		return pipelineError(c, e)
	}
	return utils.Success(c, 200, d)
}

func (h *PipelineHandler) finalize(c *fiber.Ctx) error {
	if _, err := uuid.Parse(c.Params("id")); err != nil {
		return utils.Error(c, 404, "NOT_FOUND", "ไม่พบงานที่ต้องการ", nil)
	}
	var in req.FinalizePipelineRequest
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF || validateRequest(in) != nil {
		return utils.Error(c, 400, "VALIDATION_ERROR", "กรุณาตรวจสอบข้อมูลคำขอ", nil)
	}
	d, err := h.facade.Finalize(c.Context(), c.Params("id"), c.Locals("userID").(string), in)
	if err != nil {
		return pipelineError(c, err)
	}
	return utils.Success(c, 201, d)
}
