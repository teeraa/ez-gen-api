package modelark

import (
	"context"
	"errors"
	"net"
	"strings"

	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
)

type ImageError struct {
	Code       string
	Message    string
	diagnostic ImageDiagnostic
}

type ImageDiagnostic struct {
	HTTPStatus   int
	ProviderCode string
	Param        string
}

func (e *ImageError) Error() string { return e.Message }

// ImageFailureDiagnostic excludes free-form provider messages and request IDs.
func ImageFailureDiagnostic(err error) ImageDiagnostic {
	return classifyImageError(err).(*ImageError).diagnostic
}

func safeImageParam(param *string) string {
	if param != nil {
		switch *param {
		case "model", "prompt", "image", "size", "response_format", "watermark", "sequential_image_generation", "output_format":
			return *param
		}
	}
	return ""
}

func imageFailure(code string, status int, param *string) *ImageError {
	e := &ImageError{Code: "PROVIDER_ERROR", Message: "สร้างภาพไม่สำเร็จ กรุณาตรวจสอบสถานะบริการแล้วลองอีกครั้ง"}
	family, _, _ := strings.Cut(code, ".")
	// Only known families survive; provider-controlled suffixes may contain user data.
	safeCode := safeVideoCode(code)
	if family == "OutputImageSensitiveContentDetected" {
		safeCode = family
	}
	if status < 100 || status > 599 {
		status = 0
	}
	e.diagnostic = ImageDiagnostic{HTTPStatus: status, ProviderCode: safeCode, Param: safeImageParam(param)}
	lower := strings.ToLower(code)
	switch {
	case family == "InputTextSensitiveContentDetected":
		e.Code = "PROVIDER_INVALID_INPUT"
		e.Message = "บริการสร้างภาพปฏิเสธคำอธิบายจากการตรวจเนื้อหา กรุณาปรับคำอธิบายแล้วลองใหม่"
	case family == "InputImageSensitiveContentDetected":
		e.Code = "PROVIDER_INVALID_INPUT"
		e.Message = "บริการสร้างภาพปฏิเสธภาพอ้างอิงจากการตรวจเนื้อหา กรุณาเปลี่ยนภาพอ้างอิง"
	case family == "OutputImageSensitiveContentDetected":
		e.Code = "PROVIDER_INVALID_INPUT"
		e.Message = "ภาพที่สร้างถูกปฏิเสธจากการตรวจเนื้อหา กรุณาปรับคำอธิบายหรือภาพอ้างอิง"
	case status == 401 || status == 403:
		e.Code = "PROVIDER_ACCESS_DENIED"
		e.Message = "กรุณาติดต่อผู้ดูแลระบบเพื่อตรวจสอบคีย์บริการและการเปิดใช้งานโมเดล"
	case strings.Contains(lower, "quota") || strings.Contains(lower, "balance"):
		e.Code = "PROVIDER_QUOTA"
		e.Message = "โควตาหรือยอดคงเหลือของบริการสร้างภาพหมด กรุณาติดต่อผู้ดูแลระบบ"
	case status == 429:
		e.Code = "PROVIDER_RATE_LIMIT"
		e.Message = "บริการสร้างภาพมีคำขอจำนวนมาก กรุณารอสักครู่แล้วลองใหม่"
	case status == 404 || strings.Contains(lower, "model"):
		e.Code = "MODEL_UNAVAILABLE"
		e.Message = "โมเดลสร้างภาพยังใช้งานไม่ได้ กรุณาติดต่อผู้ดูแลระบบเพื่อตรวจสอบการเปิดใช้งาน"
	case status == 400 || family == "InvalidParameter" || family == "MissingParameter" || family == "InvalidRequest":
		e.Code = "PROVIDER_INVALID_INPUT"
		e.Message = "บริการสร้างภาพไม่รับข้อมูลนี้ กรุณาปรับคำอธิบาย ภาพอ้างอิง หรือการตั้งค่า"
		if e.diagnostic.Param != "" {
			e.Message = "บริการสร้างภาพไม่รับข้อมูลในช่อง " + e.diagnostic.Param + " กรุณาตรวจคำอธิบาย ภาพอ้างอิง หรือการตั้งค่าช่องนี้"
		}
	}
	return e
}

func classifyImageError(err error) error {
	var known *ImageError
	if errors.As(err, &known) {
		return known
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return &ImageError{Code: "PROVIDER_TIMEOUT", Message: "บริการสร้างภาพใช้เวลานานเกินกำหนด กรุณาตรวจสอบยอดใช้งานก่อนลองใหม่"}
	}
	var api *sdkmodel.APIError
	if errors.As(err, &api) {
		return imageFailure(api.Code, api.HTTPStatusCode, api.Param)
	}
	return imageFailure("", 0, nil)
}
