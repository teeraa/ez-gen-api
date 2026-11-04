package modelark

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
)

func TestVideoDiagnosticPreservesOnlySafeMetadata(t *testing.T) {
	param := "content[12].image_url.url"
	err := classifyVideoError(fmt.Errorf("private wrapper: %w", &sdkmodel.APIError{
		HTTPStatusCode: 400, Code: "InvalidParameter.TaskTypeConstraint", Param: &param,
		Message: "secret-key private-prompt https://private.invalid/?signed=secret", RequestId: "secret-request-id",
	}))
	d := VideoFailureDiagnostic(fmt.Errorf("another private wrapper: %w", err))
	if d.HTTPStatus != 400 || d.ProviderCode != "InvalidParameter" || d.Param != param {
		t.Fatalf("diagnostic %+v", d)
	}
	if !strings.Contains(err.Message, param) {
		t.Fatalf("missing field hint: %s", err.Message)
	}
	logged := fmt.Sprintf("status=%d code=%q param=%q", d.HTTPStatus, d.ProviderCode, d.Param)
	for _, private := range []string{"secret", "private", "TaskTypeConstraint"} {
		if strings.Contains(logged, private) || strings.Contains(err.Error(), private) {
			t.Fatalf("unsafe diagnostic %s %s", logged, err)
		}
	}
}

func TestVideoDiagnosticRejectsUntrustedFields(t *testing.T) {
	for _, param := range []string{"secret-key", "content[1000].image_url.url", "content[-1].text", "content[01].text", "content[0].image_url.url\nsecret", "content[0].text.private", "https://private.invalid"} {
		err := classifyVideoError(&sdkmodel.APIError{HTTPStatusCode: 400, Code: "secret-key", Param: &param, Message: "secret-body"})
		d := VideoFailureDiagnostic(err)
		if d.Param != "" || d.ProviderCode != "" || strings.Contains(err.Message, param) {
			t.Fatalf("unsafe diagnostic %+v %s", d, err)
		}
	}
	param := "resolution"
	err := classifyVideoError(&sdkmodel.APIError{HTTPStatusCode: 400, Code: "InvalidParameter.secret-key", Param: &param})
	if d := VideoFailureDiagnostic(err); d.ProviderCode != "InvalidParameter" || d.Param != param {
		t.Fatalf("diagnostic %+v", d)
	}
}

func TestVideoDiagnosticNilAndTimeout(t *testing.T) {
	for _, err := range []error{nil, context.DeadlineExceeded, &sdkmodel.APIError{HTTPStatusCode: 400}} {
		d := VideoFailureDiagnostic(err)
		if d.Param != "" || d.ProviderCode != "" {
			t.Fatalf("unexpected metadata %+v", d)
		}
	}
	if code, _ := VideoFailure(context.DeadlineExceeded); code != "PROVIDER_TIMEOUT" {
		t.Fatal(code)
	}
}

func TestVideoInputImageRejectedDiagnostic(t *testing.T) {
	for _, code := range []string{"InputImageSensitiveContentDetected", "InputImageSensitiveContentDetected.Detail"} {
		err := classifyVideoError(fmt.Errorf("private wrapper: %w", &sdkmodel.APIError{HTTPStatusCode: 400, Code: code, Message: "secret private image URL"}))
		if err.Code != "PROVIDER_IMAGE_REJECTED" || !strings.Contains(err.Message, "ตรวจเนื้อหา") || !strings.Contains(err.Message, "หากเป็นภาพใบหน้าคนจริง") {
			t.Fatalf("missing conditional image guidance: %s %s", err.Code, err.Message)
		}
		d := VideoFailureDiagnostic(err)
		if d.HTTPStatus != 400 || d.ProviderCode != "InputImageSensitiveContentDetected" || d.Param != "" {
			t.Fatalf("diagnostic %+v", d)
		}
		if strings.Contains(err.Message, "secret") || strings.Contains(err.Message, "URL") {
			t.Fatalf("unsafe message: %s", err.Message)
		}
	}
	for _, code := range []string{"NotInputImageSensitiveContentDetected", "InputImageSensitiveContentDetectedSecret"} {
		if err := classifyVideoError(&sdkmodel.APIError{HTTPStatusCode: 400, Code: code}); err.Code != "PROVIDER_INVALID_INPUT" {
			t.Fatalf("overbroad classification %s", code)
		}
	}
}
