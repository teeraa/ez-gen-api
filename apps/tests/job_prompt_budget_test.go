package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/google/uuid"
)

func TestJobVideoPromptBudgetFailsBeforePersistenceOrProvider(t *testing.T) {
	t.Parallel()
	jobs := newFakeJobRepo()
	provider := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("")
	f := facade.NewJobFacade(jobs, nil, nil, nil, nil, nil, provider, registry, nil, nil, 3)
	_, e := f.CreateJob(context.Background(), uuid.NewString(), facade.CreateJobInput{Prompt: strings.Repeat("ก", promptcompiler.VideoPromptRuneLimit+1), Mode: "text", ModelID: "violet-motion-2", AspectRatio: "16:9", DurationSeconds: 5, Quality: "standard"})
	if !errors.Is(e, facade.ErrVideoPromptBudget) || len(jobs.jobs) != 0 || len(provider.CreatedRequests) != 0 {
		t.Fatalf("overbudget video incurred persistence/provider work err=%v", e)
	}
}
