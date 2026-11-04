package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

type terminalProjectionStore struct {
	*pipelineMemory
	writes int
}

func (s *terminalProjectionStore) Get(_ context.Context, id, user string) (*models.StudioPipeline, error) {
	if s.p.ID.String() != id || s.p.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return s.p, nil
}
func (s *terminalProjectionStore) Advance(c context.Context, p *models.StudioPipeline) error {
	s.writes++
	return s.pipelineMemory.Advance(c, p)
}
func (s *terminalProjectionStore) Retry(c context.Context, id, user string) error {
	s.writes++
	return s.pipelineMemory.Retry(c, id, user)
}

type terminalProjectionReader struct {
	pipelineJobReader
	owner      string
	calls      int
	wrongOwner bool
}

func (r *terminalProjectionReader) FindByID(c context.Context, id, user string) (*models.VideoGenerationJob, error) {
	r.calls++
	if user != r.owner {
		r.wrongOwner = true
	}
	return r.pipelineJobReader.FindByID(c, id, user)
}

func TestPipelineTerminalEvidenceClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code, providerStatus, task string
		job                              models.JobStatus
		wantStatus, wantCode             string
	}{
		{"legacy known failure", "PROVIDER_ERROR", "failed", "accepted-task", models.JobStatusFailed, "failed", "PROVIDER_GENERATION_FAILED"},
		{"legacy known cancelled", "PROVIDER_ERROR", "cancelled", "accepted-task", models.JobStatusCancelled, "failed", "PROVIDER_GENERATION_FAILED"},
		{"unknown submission", "PROVIDER_ERROR", "", "", models.JobStatusFailed, "needs_attention", "PROVIDER_ERROR"},
		{"missing task", "PROVIDER_ERROR", "failed", "", models.JobStatusFailed, "needs_attention", "PROVIDER_ERROR"},
		{"provider still running", "PROVIDER_ERROR", "running", "accepted-task", models.JobStatusFailed, "needs_attention", "PROVIDER_ERROR"},
		{"job nonterminal", "PROVIDER_ERROR", "failed", "accepted-task", models.JobStatusProcessing, "processing", ""},
		{"policy rejection", "PROVIDER_OUTPUT_REJECTED", "failed", "accepted-task", models.JobStatusFailed, "failed", "PROVIDER_OUTPUT_REJECTED"},
		{"typed terminal failure", "PROVIDER_GENERATION_FAILED", "failed", "accepted-task", models.JobStatusFailed, "failed", "PROVIDER_GENERATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &models.StudioPipeline{Status: "processing", Stage: "video"}
			j := &models.VideoGenerationJob{Status: tc.job, FailureCode: &tc.code, ProviderTaskID: &tc.task, ProviderStatus: &tc.providerStatus}
			if err := pipelineChild(p, j, "save_video"); err != nil {
				t.Fatal(err)
			}
			if p.Status != tc.wantStatus || strValOrDefault(p.FailureCode, "") != tc.wantCode {
				t.Fatalf("classification %+v", p)
			}
			if tc.wantStatus == "failed" && strValOrDefault(p.FailureMessage, "") == "" {
				t.Fatal("missing safe terminal copy")
			}
		})
	}
}
func TestPipelineLegacyTerminalProjectionIsReadOnlyAndScoped(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"known", "unknown", "foreign-child"} {
		t.Run(kind, func(t *testing.T) {
			f, m, _, _, user, in := pipelineFixture(t)
			created, err := f.Create(context.Background(), user, in)
			if err != nil {
				t.Fatal(err)
			}
			task, providerStatus, code, oldMessage := "accepted-task", "failed", "PROVIDER_ERROR", "legacy-private-provider-message"
			childID := uuid.New()
			uid := uuid.MustParse(user)
			child := &models.VideoGenerationJob{Base: models.Base{ID: childID}, UserID: uid, Status: models.JobStatusFailed, ProviderTaskID: &task, ProviderStatus: &providerStatus, FailureCode: &code}
			if kind == "unknown" {
				child.ProviderTaskID = nil
			}
			if kind == "foreign-child" {
				child.UserID = uuid.New()
			}
			m.js[childID.String()] = child
			m.p.VideoJobID = &childID
			m.p.Stage = "video"
			m.p.Status = "needs_attention"
			m.p.FailureCode = &code
			m.p.FailureMessage = &oldMessage
			store := &terminalProjectionStore{pipelineMemory: m}
			reader := &terminalProjectionReader{pipelineJobReader: m, owner: user}
			f.store = store
			f.reader = reader
			before, _ := json.Marshal(m.p)
			got, err := f.Get(context.Background(), created.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "known" {
				if got.Status != "failed" || strValOrDefault(got.FailureCode, "") != "PROVIDER_GENERATION_FAILED" || got.FailureMessage == nil || *got.FailureMessage == oldMessage {
					t.Fatalf("known terminal not projected %+v", got)
				}
			} else if got.Status != "needs_attention" {
				t.Fatalf("uncertain result reclassified %+v", got)
			}
			if got.CanRetry {
				t.Fatal("historical projection enabled paid retry")
			}
			list, err := f.List(context.Background(), user, 1, 20)
			if err != nil || len(list.Items) != 1 || list.Items[0].Status != got.Status {
				t.Fatalf("list projection mismatch %+v %v", list, err)
			}
			after, _ := json.Marshal(m.p)
			if string(before) != string(after) || store.writes != 0 {
				t.Fatalf("read mutated persisted aggregate writes%d", store.writes)
			}
			if reader.calls == 0 || reader.wrongOwner {
				t.Fatal("child lookup not scoped to pipeline owner")
			}
			if _, err = f.Get(context.Background(), created.ID, uuid.NewString()); !errors.Is(err, ErrNotFound) {
				t.Fatal("foreign pipeline exposed", err)
			}
		})
	}
}
func TestPipelineTerminalFailureCannotRetrySameRequest(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"PROVIDER_OUTPUT_REJECTED", "PROVIDER_GENERATION_FAILED"} {
		t.Run(code, func(t *testing.T) {
			f, m, _, _, user, in := pipelineFixture(t)
			got, err := f.Create(context.Background(), user, in)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.New()
			m.p.VideoJobID = &id
			m.p.Status = "failed"
			m.p.Stage = "video"
			m.p.FailureCode = &code
			view, err := f.Get(context.Background(), got.ID, user)
			if err != nil || view.CanRetry {
				t.Fatalf("unsafe retry availability %+v %v", view, err)
			}
			if _, err = f.Retry(context.Background(), got.ID, user); !errors.Is(err, ErrConflict) {
				t.Fatal("same paid request retry allowed", err)
			}
		})
	}
}
