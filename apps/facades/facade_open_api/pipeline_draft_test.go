package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

func TestPipelineDraftForwardingAndResolvedModel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, model, override string
		draft, allowed        bool
	}{
		{"draft", "violet-realism", "", true, true},
		{"normal", "violet-realism", "", false, true},
		{"unsupported alias", "violet-motion-2", "", true, false},
		{"override enables", "violet-motion-2", "dreamina-seedance-2-5-260628", true, true},
		{"override disables", "violet-realism", "dreamina-seedance-2-0-fast-260128", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, m, _, videos, user, in := pipelineFixture(t)
			registry, err := modelark.NewRegistry(tc.override)
			if err != nil {
				t.Fatal(err)
			}
			f.registry = registry
			in.ModelID = tc.model
			in.Draft = tc.draft
			got, err := f.Create(context.Background(), user, in)
			if !tc.allowed {
				if !errors.Is(err, ErrModelInvalid) || m.p != nil {
					t.Fatalf("unsupported draft persisted: %+v %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.IsDraft != tc.draft || got.ModelID != tc.model || got.ModelLabel == "" {
				t.Fatalf("draft snapshot missing %+v", got)
			}
			restarted := *f
			runPipelineTicks(t, &restarted, 6)
			if videos.calls != 1 || videos.seen.Draft != tc.draft {
				t.Fatalf("draft lost at child request %+v", videos.seen)
			}
			if _, err = f.Create(context.Background(), user, in); err != nil {
				t.Fatal("replay", err)
			}
			in.Draft = !tc.draft
			if _, err = f.Create(context.Background(), user, in); !errors.Is(err, ErrIdempotencyMismatch) {
				t.Fatalf("draft not part of identity %v", err)
			}
		})
	}
}

type finalPipelineMemory struct {
	*pipelineMemory
	source *models.StudioPipeline
}

func (m *finalPipelineMemory) Get(c context.Context, id, user string) (*models.StudioPipeline, error) {
	if m.source.ID.String() == id && m.source.UserID.String() == user {
		return clonePipeline(m.source), nil
	}
	return m.pipelineMemory.Get(c, id, user)
}

func completedDraftFixture(t *testing.T) (*PipelineFacade, *finalPipelineMemory, *pipelineVideo, string, *dto.PipelineDTO) {
	t.Helper()
	f, m, _, videos, user, in := pipelineFixture(t)
	in.ModelID = "violet-realism"
	in.Draft = true
	d, e := f.Create(context.Background(), user, in)
	if e != nil {
		t.Fatal(e)
	}
	runPipelineTicks(t, f, 6)
	task := "private-provider-draft-task"
	m.js[m.p.VideoJobID.String()].ProviderTaskID = &task
	m.js[m.p.VideoJobID.String()].CreatedAt = time.Now()
	m.js[m.p.VideoJobID.String()].EffectiveInputs = []byte(`{"providerModelId":"dreamina-seedance-2-5-260628"}`)
	store := &finalPipelineMemory{pipelineMemory: m, source: clonePipeline(m.p)}
	f.store = store
	m.p = nil
	return f, store, videos, user, d
}
func TestPipelineFinalizePreservesDraftAndRefinesIdempotently(t *testing.T) {
	t.Parallel()
	f, m, videos, user, source := completedDraftFixture(t)
	c := context.Background()
	in := req.FinalizePipelineRequest{IdempotencyKey: uuid.NewString()}
	got, e := f.Finalize(c, source.ID, user, in)
	if e != nil {
		t.Fatal(e)
	}
	if got.ID == source.ID || got.IsDraft || got.Quality != "high" || got.SourceDraftID == nil || *got.SourceDraftID != source.ID {
		t.Fatalf("invalid final %+v", got)
	}
	var snapshot pipelineSnapshot
	if e = json.Unmarshal(m.p.Inputs, &snapshot); e != nil {
		t.Fatal(e)
	}
	if snapshot.VideoModel.ProviderModelID != "dreamina-seedance-2-5-260628" || snapshot.SourceDraftJobID != m.source.VideoJobID.String() {
		t.Fatal("source identity not frozen")
	}
	if m.source.Status != "succeeded" {
		t.Fatal("draft mutated")
	}
	runPipelineTicks(t, f, 4)
	if videos.calls != 2 || videos.seen.Draft || videos.seen.draftSourceJobID != m.source.VideoJobID.String() || videos.seen.ReferenceAssetID != nil {
		t.Fatalf("not a refine request %+v", videos.seen)
	}
	again, e := f.Finalize(c, source.ID, user, in)
	if e != nil || again.ID != got.ID {
		t.Fatalf("duplicate final %+v %v", again, e)
	}
	if _, e = f.Finalize(c, source.ID, uuid.NewString(), in); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign source", e)
	}
}
func TestPipelineFinalizeRejectsUnreadyAndExpiredDraft(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"expired", "non-draft", "pending", "provider-missing"} {
		t.Run(kind, func(t *testing.T) {
			f, m, _, user, source := completedDraftFixture(t)
			job := m.js[m.source.VideoJobID.String()]
			switch kind {
			case "expired":
				job.CreatedAt = time.Now().Add(-15 * 24 * time.Hour)
			case "non-draft":
				job.IsDraft = false
			case "pending":
				m.source.Status = "processing"
			case "provider-missing":
				job.ProviderTaskID = nil
			}
			if _, e := f.Finalize(context.Background(), source.ID, user, req.FinalizePipelineRequest{IdempotencyKey: uuid.NewString()}); e == nil {
				t.Fatal("ineligible source accepted")
			}
			if m.p != nil {
				t.Fatal("ineligible source persisted")
			}
		})
	}
}
func TestPipelineCapabilitiesResolvedLabels(t *testing.T) {
	t.Parallel()
	f, _, _, _, _, _ := pipelineFixture(t)
	reg, _ := modelark.NewRegistry("dreamina-seedance-2-5-260628")
	f.registry = reg
	caps := f.Capabilities()
	if len(caps.Models) != 5 {
		t.Fatal("missing models")
	}
	for _, m := range caps.Models {
		if !m.SupportsDraft || m.Label != "Seedance 2.5" || len(m.Durations) == 0 {
			t.Fatalf("incorrect resolved metadata %+v", m)
		}
	}
}

var _ pipelineStore = (*finalPipelineMemory)(nil)
