package modelark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/volcengine/ark-runtime-go/arkruntime/model/contentgeneration"
)

func TestSDKTerminalGenerationFailureIsBoundedAndDistinct(t *testing.T) {
	for _, tc := range []struct{ name, providerCode, want, wantFamily string }{
		{"output_policy", "OutputVideoSensitiveContentDetected.PolicyViolation", "PROVIDER_OUTPUT_REJECTED", "OutputVideoSensitiveContentDetected"},
		{"input_policy_unchanged", "InputImageSensitiveContentDetected.PolicyViolation", "PROVIDER_IMAGE_REJECTED", "InputImageSensitiveContentDetected"},
		{"unknown", "private-secret-code", "PROVIDER_GENERATION_FAILED", ""},
		{"missing_error", "", "PROVIDER_GENERATION_FAILED", ""},
		{"unknown_model_hint", "UnknownModelRuntimeError.private", "PROVIDER_GENERATION_FAILED", ""},
		{"unknown_quota_hint", "UnknownQuotaRuntimeError.private", "PROVIDER_GENERATION_FAILED", ""},
		{"unknown_balance_hint", "UnknownBalanceRuntimeError.private", "PROVIDER_GENERATION_FAILED", ""},
		{"known_model", "ModelNotFound.Unavailable", "MODEL_UNAVAILABLE", "ModelNotFound"},
		{"known_quota", "QuotaExceeded.Limit", "PROVIDER_QUOTA", "QuotaExceeded"},
		{"known_balance", "InsufficientBalance.Limit", "PROVIDER_QUOTA", "InsufficientBalance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &contentgeneration.ContentGenerationTask{ID: "fixture-task", Status: contentgeneration.TaskStatusFailed}
			if tc.providerCode != "" {
				task.Error = contentgeneration.NewOptTaskError(contentgeneration.TaskError{Code: tc.providerCode, Message: "secret-key private-prompt https://private.invalid/?signed=secret"})
			}
			got := fromSDKTask(task)
			if got.ErrorCode != tc.want || got.FailureDiagnostic.Phase != "generation" || got.FailureDiagnostic.ProviderCode != tc.wantFamily {
				t.Fatalf("unexpected safe failure %+v", got)
			}
			diagnostic, e := json.Marshal(got.FailureDiagnostic)
			if e != nil {
				t.Fatal(e)
			}
			for _, private := range []string{"secret", "private", "PolicyViolation", "request_id", "message", "url"} {
				if strings.Contains(string(diagnostic), private) || strings.Contains(got.ErrorMessage, private) {
					t.Fatalf("private data leaked: %s", diagnostic)
				}
			}
			if tc.want == "PROVIDER_OUTPUT_REJECTED" && (!strings.Contains(got.ErrorMessage, "นโยบายเนื้อหา") || strings.Contains(got.ErrorMessage, "ใบหน้า") || strings.Contains(got.ErrorMessage, "ลิขสิทธิ์")) {
				t.Fatal("unsupported cause in policy message")
			}
		})
	}
}

func TestGenerationFailureDoesNotRelabelSubmissionOrRunning(t *testing.T) {
	for _, status := range []contentgeneration.TaskStatus{contentgeneration.TaskStatusQueued, contentgeneration.TaskStatusRunning, contentgeneration.TaskStatusSucceeded} {
		got := fromSDKTask(&contentgeneration.ContentGenerationTask{ID: "fixture", Status: status})
		if got.ErrorCode != "" || got.FailureDiagnostic.Phase != "" {
			t.Fatalf("nonfailure changed %+v", got)
		}
	}
	if code, _ := VideoFailure(errors.New("private unexpected submit")); code != "PROVIDER_ERROR" {
		t.Fatal(code)
	}
	if code, _ := VideoFailure(context.DeadlineExceeded); code != "PROVIDER_TIMEOUT" {
		t.Fatal(code)
	}
}
