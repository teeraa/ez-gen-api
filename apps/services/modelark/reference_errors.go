package modelark

import (
	"context"
	"errors"
	"net"
	"regexp"

	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
)

type ReferenceDiagnostic struct {
	Stage        string
	HTTPStatus   int
	ProviderCode string
	Param        string
	FinishReason string
	ContentBytes int
}

type ReferenceAnalysisError struct {
	Code       string
	Message    string
	diagnostic ReferenceDiagnostic
	cause      error
}

func (e *ReferenceAnalysisError) Error() string { return e.Message }
func (e *ReferenceAnalysisError) Unwrap() error { return e.cause }

func referenceFailure(code, message, stage string) *ReferenceAnalysisError {
	return &ReferenceAnalysisError{Code: code, Message: message, diagnostic: ReferenceDiagnostic{Stage: stage}}
}

var referenceParamPath = regexp.MustCompile(`^messages\[(0|[1-9][0-9]{0,2})\]\.(role|content|content\[(0|[1-9][0-9]{0,2})\]\.(type|text|image_url(\.url)?))$`)

func safeReferenceParam(param *string) string {
	if param == nil {
		return ""
	}
	switch *param {
	case "model", "messages", "max_tokens", "thinking", "thinking.type", "reasoning_effort", "temperature":
		return *param
	}
	if referenceParamPath.MatchString(*param) {
		return *param
	}
	return ""
}

func classifyReferenceError(err error) *ReferenceAnalysisError {
	var known *ReferenceAnalysisError
	if errors.As(err, &known) {
		return known
	}
	e := referenceFailure("REFERENCE_ANALYSIS_FAILED", "วิเคราะห์ภาพอ้างอิงไม่สำเร็จ กรุณาลองใหม่หรือสร้างจากข้อความอย่างเดียว", "provider")
	var ne net.Error
	if errors.Is(err, context.Canceled) {
		e.Code, e.Message, e.cause = "REFERENCE_ANALYSIS_CANCELLED", "การวิเคราะห์ภาพอ้างอิงถูกยกเลิก กรุณาลองใหม่", context.Canceled
	} else if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		e.Code, e.Message = "REFERENCE_ANALYSIS_TIMEOUT", "บริการวิเคราะห์ภาพใช้เวลานานเกินกำหนด กรุณาตรวจยอดใช้งานก่อนลองใหม่"
		if errors.Is(err, context.DeadlineExceeded) {
			e.cause = context.DeadlineExceeded
		}
	} else {
		var api *sdkmodel.APIError
		if errors.As(err, &api) {
			status := api.HTTPStatusCode
			if status < 100 || status > 599 {
				status = 0
			}
			e.diagnostic.HTTPStatus, e.diagnostic.ProviderCode, e.diagnostic.Param = status, safeVideoCode(api.Code), safeReferenceParam(api.Param)
			switch status {
			case 400:
				e.Code, e.Message = "REFERENCE_ANALYSIS_INVALID_INPUT", "บริการวิเคราะห์ภาพไม่รับข้อมูลนี้ กรุณาตรวจภาพอ้างอิงและการตั้งค่าโมเดลวิเคราะห์"
			case 401, 403:
				e.Code, e.Message = "REFERENCE_ANALYSIS_ACCESS_DENIED", "บริการวิเคราะห์ภาพไม่อนุญาตการเข้าถึง กรุณาติดต่อผู้ดูแลเพื่อตรวจคีย์และสิทธิ์โมเดล"
			case 404:
				e.Code, e.Message = "REFERENCE_ANALYSIS_MODEL_UNAVAILABLE", "ไม่พบโมเดลวิเคราะห์ภาพ กรุณาติดต่อผู้ดูแลเพื่อตรวจชื่อโมเดลและการเปิดใช้งาน"
			case 429:
				e.Code, e.Message = "REFERENCE_ANALYSIS_RATE_LIMIT", "บริการวิเคราะห์ภาพจำกัดคำขอ กรุณาตรวจโควตาและรอสักครู่ก่อนลองใหม่"
			}
			if status == 400 && e.diagnostic.Param != "" {
				e.Message += " (" + e.diagnostic.Param + ")"
			}
		}
	}
	return e
}

// ReferenceFailure exposes only finite categories and validated metadata.
func ReferenceFailure(err error) (string, string, ReferenceDiagnostic) {
	e := classifyReferenceError(err)
	return e.Code, e.Message, e.diagnostic
}

func referenceOutputFailure(reason string, bytes int) *ReferenceAnalysisError {
	e := referenceFailure("REFERENCE_ANALYSIS_INVALID_OUTPUT", "บริการวิเคราะห์ภาพส่งคำอธิบายที่ใช้ไม่ได้ กรุณาเปลี่ยนภาพหรือสร้างจากข้อความอย่างเดียว", "output")
	switch reason {
	case "stop", "length", "content_filter", "tool_calls", "function_call":
		e.diagnostic.FinishReason = reason
	case "empty", "refusal":
		e.diagnostic.FinishReason = "stop"
	}
	e.diagnostic.ContentBytes = bytes
	switch reason {
	case "length":
		e.Code, e.Message = "REFERENCE_ANALYSIS_OUTPUT_LIMIT", "คำตอบวิเคราะห์ภาพเกินขีดจำกัด กรุณาติดต่อผู้ดูแลเพื่อตรวจการตั้งค่าโมเดลวิเคราะห์"
	case "content_filter", "refusal":
		e.Code, e.Message = "REFERENCE_ANALYSIS_REFUSED", "บริการวิเคราะห์ภาพปฏิเสธคำขอนี้ กรุณาเปลี่ยนภาพหรือสร้างจากข้อความอย่างเดียว"
	case "empty":
		e.Code, e.Message = "REFERENCE_ANALYSIS_EMPTY_OUTPUT", "บริการวิเคราะห์ภาพไม่ได้ส่งคำอธิบาย กรุณาตรวจการตั้งค่าโมเดลหรือสร้างจากข้อความอย่างเดียว"
	}
	return e
}
