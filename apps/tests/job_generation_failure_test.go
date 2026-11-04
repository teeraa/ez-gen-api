package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"gorm.io/datatypes"
)

type terminalFailureProvider struct {
	modelark.Client
	result        *modelark.TaskResult
	err           error
	creates, gets int
}

func (a *terminalFailureProvider) CreateTask(context.Context, modelark.CreateTaskRequest) (string, error) {
	a.creates++
	return "", errors.New("unexpected create")
}
func (a *terminalFailureProvider) GetTask(context.Context, string) (*modelark.TaskResult, error) {
	a.gets++
	return a.result, a.err
}

func TestPollTerminalFailurePersistsSafeGenerationDiagnostic(t *testing.T) {
	job := newOutputJob(models.JobStatusProcessing, 0)
	repo := newFakeJobRepo(job)
	provider := &terminalFailureProvider{result: &modelark.TaskResult{Status: modelark.ProviderStatusFailed, ErrorCode: "PROVIDER_OUTPUT_REJECTED", ErrorMessage: "ผู้ให้บริการไม่อนุญาตวิดีโอที่สร้างตามนโยบายเนื้อหา", FailureDiagnostic: modelark.GenerationFailureDiagnostic{Phase: "secret-invalid-phase", ProviderCode: "OutputVideoSensitiveContentDetected.PolicyViolation"}}}
	registry, _ := modelark.NewRegistry("")
	facade := facade_open_api.NewJobFacade(repo, nil, nil, nil, nil, nil, provider, registry, nil, nil, 3)
	if e := facade.PollAndUpdate(context.Background(), job); e != nil {
		t.Fatal(e)
	}
	if provider.gets != 1 || provider.creates != 0 || len(repo.statusUpdates) != 1 || repo.statuses[0] != models.JobStatusFailed {
		t.Fatal("unexpected provider operations or transition")
	}
	update := repo.statusUpdates[0]
	if update["provider_status"] != "failed" || update["failure_code"] != "PROVIDER_OUTPUT_REJECTED" {
		t.Fatalf("lost terminal evidence %+v", update)
	}
	encoded, ok := update["provider_error"].(datatypes.JSON)
	if !ok {
		t.Fatalf("diagnostic type %T", update["provider_error"])
	}
	var detail map[string]string
	if e := json.Unmarshal(encoded, &detail); e != nil {
		t.Fatal(e)
	}
	if len(detail) != 2 || detail["phase"] != "generation" || detail["providerCode"] != "OutputVideoSensitiveContentDetected" || strings.Contains(string(encoded), "PolicyViolation") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("unsafe diagnostic %s", encoded)
	}
}

func TestPollHTTPFailureDoesNotPersistTerminalFailure(t *testing.T) {
	job := newOutputJob(models.JobStatusProcessing, 0)
	repo := newFakeJobRepo(job)
	provider := &terminalFailureProvider{err: errors.New("fixture polling transport failure")}
	registry, _ := modelark.NewRegistry("")
	facade := facade_open_api.NewJobFacade(repo, nil, nil, nil, nil, nil, provider, registry, nil, nil, 3)
	if e := facade.PollAndUpdate(context.Background(), job); e == nil {
		t.Fatal("polling failure ignored")
	}
	if len(repo.statusUpdates) != 0 || provider.creates != 0 || provider.gets != 1 {
		t.Fatal("HTTP uncertainty became a terminal generation result")
	}
}
