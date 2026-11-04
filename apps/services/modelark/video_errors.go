package modelark

import (
	"context"
	"errors"
	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
	"net"
	"regexp"
	"strings"
)

type VideoError struct {
	Code       string
	Message    string
	diagnostic VideoDiagnostic
}

type VideoDiagnostic struct {
	HTTPStatus   int
	ProviderCode string
	Param        string
}

var videoParamPath = regexp.MustCompile(`^content\[(0|[1-9][0-9]{0,2})\]\.(type|text|role|image_url(\.url)?|video_url(\.url)?|audio_url(\.url)?)$`)

func safeVideoParam(param *string) string {
	if param == nil {
		return ""
	}
	switch *param {
	case "model", "content", "ratio", "duration", "resolution", "optimize_prompt", "generate_audio", "watermark", "return_last_frame", "execution_expires_after":
		return *param
	}
	if videoParamPath.MatchString(*param) {
		return *param
	}
	return ""
}

func safeVideoCode(code string) string {
	family, _, _ := strings.Cut(code, ".")
	switch family {
	case "InvalidParameter", "MissingParameter", "InvalidRequest", "AccessDenied", "AuthenticationError", "Unauthorized", "ModelNotFound", "ResourceNotFound", "RateLimit", "RateLimitExceeded", "QuotaExceeded", "InsufficientBalance", "InternalError", "InternalServiceError", "ServerOverloaded", "InputImageSensitiveContentDetected", "InputVideoSensitiveContentDetected", "InputTextSensitiveContentDetected", "OutputVideoSensitiveContentDetected":
		return family
	}
	return ""
}

// VideoFailureDiagnostic omits free-form provider data, including request IDs.
func VideoFailureDiagnostic(err error) VideoDiagnostic { return classifyVideoError(err).diagnostic }

func (e *VideoError) Error() string { return e.Message }
func (e *VideoError) Unwrap() error { return ErrProviderError }
func videoFailure(code string, status int) *VideoError {
	e := &VideoError{Code: "PROVIDER_ERROR", Message: "สร้างวิดีโอไม่สำเร็จ กรุณาตรวจสถานะบริการแล้วลองใหม่"}
	family, _, _ := strings.Cut(code, ".")
	code = strings.ToLower(code)
	switch {
	case family == "OutputVideoSensitiveContentDetected":
		e.Code = "PROVIDER_OUTPUT_REJECTED"
		e.Message = "ผู้ให้บริการไม่อนุญาตวิดีโอที่สร้างตามนโยบายเนื้อหา กรุณาปรับฉากหรือภาพอ้างอิงแล้วสร้างคำขอใหม่"
	case family == "InputImageSensitiveContentDetected":
		e.Code = "PROVIDER_IMAGE_REJECTED"
		e.Message = "บริการวิดีโอปฏิเสธภาพอ้างอิงจากการตรวจเนื้อหา กรุณาเปลี่ยนภาพ หากเป็นภาพใบหน้าคนจริง ให้ใช้ภาพหรือ asset ที่โมเดลอนุญาต"
	case strings.Contains(code, "quota") || strings.Contains(code, "balance"):
		e.Code = "PROVIDER_QUOTA"
		e.Message = "โควตาหรือยอดคงเหลือของบริการวิดีโอไม่เพียงพอ กรุณาติดต่อผู้ดูแลระบบ"
	case status == 401 || status == 403:
		e.Code = "PROVIDER_ACCESS_DENIED"
		e.Message = "บริการวิดีโอไม่อนุญาตการเข้าถึง กรุณาตรวจคีย์และการเปิดใช้โมเดล"
	case status == 404 || strings.Contains(code, "model"):
		e.Code = "MODEL_UNAVAILABLE"
		e.Message = "โมเดลวิดีโอยังใช้งานไม่ได้ กรุณาตรวจการเปิดใช้โมเดล"
	case status == 429:
		e.Code = "PROVIDER_RATE_LIMIT"
		e.Message = "บริการวิดีโอมีคำขอจำนวนมาก กรุณารอสักครู่แล้วลองใหม่"
	case status == 400:
		e.Code = "PROVIDER_INVALID_INPUT"
		e.Message = "บริการวิดีโอไม่รับข้อมูลนี้ กรุณาปรับคำอธิบาย ภาพอ้างอิง หรือการตั้งค่า"
	}
	return e
}
func classifyVideoError(err error) *VideoError {
	var known *VideoError
	if errors.As(err, &known) {
		return known
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return &VideoError{Code: "PROVIDER_TIMEOUT", Message: "การส่งงานวิดีโอใช้เวลานานเกินไป กรุณาตรวจประวัติและยอดใช้งานก่อนลองใหม่"}
	}
	var api *sdkmodel.APIError
	if errors.As(err, &api) {
		e := videoFailure(api.Code, api.HTTPStatusCode)
		status := api.HTTPStatusCode
		if status < 100 || status > 599 {
			status = 0
		}
		e.diagnostic = VideoDiagnostic{HTTPStatus: status, ProviderCode: safeVideoCode(api.Code), Param: safeVideoParam(api.Param)}
		if e.Code == "PROVIDER_INVALID_INPUT" && e.diagnostic.Param != "" {
			e.Message = "บริการวิดีโอไม่รับข้อมูลในช่อง " + e.diagnostic.Param + " กรุณาตรวจคำอธิบาย ภาพอ้างอิง หรือการตั้งค่าช่องนี้"
		}
		return e
	}
	return videoFailure("", 0)
}
func VideoFailure(err error) (string, string) { e := classifyVideoError(err); return e.Code, e.Message }

// GenerationFailureDetails excludes free-form task errors and model-specific suffixes.
func GenerationFailureDetails(providerCode string) GenerationFailureDiagnostic {
	return GenerationFailureDiagnostic{Phase: "generation", ProviderCode: safeVideoCode(providerCode)}
}
