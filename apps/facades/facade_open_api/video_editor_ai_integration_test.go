package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func aiTestDB(t *testing.T, withAI bool) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("VIDEO_EDITOR_AI_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated VIDEO_EDITOR_AI_TEST_DSN")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Host != "127.0.0.1:15458" || u.Path != "/ai_editor_phase2" {
		t.Fatal("non-isolated DSN")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	schema := "ai_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		sql, _ := db.DB()
		_ = sql.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		sql, _ = admin.DB()
		_ = sql.Close()
	})
	if e = db.Exec(`CREATE TABLE users(id uuid PRIMARY KEY,created_at timestamptz DEFAULT now(),updated_at timestamptz DEFAULT now(),deleted_at timestamptz);`).Error; e != nil {
		t.Fatal(e)
	}
	files := []string{"021_video_editor.sql", "024_video_editor_audio_text.sql"}
	if withAI {
		files = append(files, "022_video_editor_ai.sql")
	}
	for _, file := range files {
		b, e := os.ReadFile("../../../migrations/" + file)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(string(b)).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db
}

type aiTestASR struct {
	calls     atomic.Int32
	entered   chan struct{}
	cancelled chan struct{}
	silent    bool
}

func (a *aiTestASR) Available() bool   { return true }
func (a *aiTestASR) Signature() string { return "test-asr-v1" }
func (a *aiTestASR) Transcribe(ctx context.Context, audio []editorai.Audio) ([]models.EditorAITranscript, error) {
	a.calls.Add(1)
	if a.entered != nil {
		close(a.entered)
		<-ctx.Done()
		close(a.cancelled)
		return nil, ctx.Err()
	}
	out := []models.EditorAITranscript{}
	for _, s := range audio {
		if a.silent {
			out = append(out, models.EditorAITranscript{AssetID: s.AssetID, Segments: []models.EditorAISpeech{}})
			continue
		}
		out = append(out, models.EditorAITranscript{AssetID: s.AssetID, Language: "th", Text: "กาแฟ", HasSpeech: true, Segments: []models.EditorAISpeech{{Start: 0, End: s.Duration, Text: "กาแฟ", Words: []models.EditorAIWord{{Start: 0, End: 0, Text: "ก"}, {Start: 0, End: s.Duration, Text: "าแฟ"}}}}})
	}
	return out, nil
}

type aiTestProvider struct {
	mu                  sync.Mutex
	calls               int
	rate                bool
	invalid             bool
	refinedText         string
	prompts             []string
	onTask              func(context.Context, string)
	malformedRefinement bool
	conceptDuration     float64
	conceptReason       string
	planAll             bool
	planReason          string
	planOverlap         bool
	distinctPlanRoles   bool
}

func (p *aiTestProvider) Available() bool   { return true }
func (p *aiTestProvider) Signature() string { return "test-gemini-v1" }
func (p *aiTestProvider) Generate(ctx context.Context, _ string, prompt string, images []editorai.Image, _ json.RawMessage) ([]byte, error) {
	p.mu.Lock()
	p.calls++
	p.prompts = append(p.prompts, prompt)
	refinedText := p.refinedText
	onTask, malformedRefinement := p.onTask, p.malformedRefinement
	rate, invalid := p.rate, p.invalid
	conceptDuration, conceptReason, planAll, planReason, planOverlap := p.conceptDuration, p.conceptReason, p.planAll, p.planReason, p.planOverlap
	distinctPlanRoles := p.distinctPlanRoles
	p.mu.Unlock()
	if rate {
		return nil, &editorai.RateError{RetryAt: time.Now().Add(time.Second)}
	}
	if invalid {
		return []byte(`{"unexpected":1}`), nil
	}
	var in struct {
		Task        string                    `json:"task"`
		Target      float64                   `json:"targetDuration"`
		Evidence    []models.EditorAIEvidence `json:"evidence"`
		Transcripts []aiRefinementSource      `json:"transcripts"`
	}
	_ = json.Unmarshal([]byte(prompt), &in)
	if onTask != nil {
		onTask(ctx, in.Task)
	}
	switch in.Task {
	case "refine_transcripts":
		out := []aiRefinementResult{}
		for _, transcript := range in.Transcripts {
			if malformedRefinement {
				transcript.Segments = nil
			}
			if refinedText != "" {
				for i := range transcript.Segments {
					transcript.Segments[i].Text = refinedText
				}
			}
			out = append(out, aiRefinementResult{AssetID: transcript.AssetID, Segments: transcript.Segments})
		}
		return aiJSON(struct {
			Transcripts []aiRefinementResult `json:"transcripts"`
		}{out}), nil
	case "vision":
		byID := map[string][]models.EditorAIVisualFrame{}
		for _, image := range images {
			byID[image.AssetID] = append(byID[image.AssetID], models.EditorAIVisualFrame{Time: image.Time, Description: "ภาพกาแฟ"})
		}
		out := []models.EditorAIVisual{}
		for id, frames := range byID {
			out = append(out, models.EditorAIVisual{AssetID: id, Summary: "กาแฟ", Frames: frames})
		}
		return aiJSON(struct {
			Visuals []models.EditorAIVisual `json:"visuals"`
		}{out}), nil
	case "concepts":
		if conceptDuration == 0 {
			conceptDuration = 1
		}
		type concept struct {
			Title          string  `json:"title"`
			Summary        string  `json:"summary"`
			Duration       float64 `json:"duration"`
			DurationReason string  `json:"durationReason,omitempty"`
		}
		return aiJSON(struct {
			Concepts []concept `json:"concepts"`
		}{[]concept{{"เรื่องแรก", "สรุปเรื่องแรก", conceptDuration, conceptReason}, {"เรื่องสอง", "สรุปเรื่องสอง", conceptDuration, conceptReason}}}), nil
	case "planning":
		if len(in.Evidence) == 0 {
			return nil, editorai.ErrInvalid
		}
		type part struct {
			EvidenceID string `json:"evidenceId"`
			Title      string `json:"title"`
			Reason     string `json:"reason"`
		}
		parts := []part{}
		selected := []models.EditorAIDraftSegment{}
		duration := 0.0
		for _, window := range in.Evidence {
			candidate := models.EditorAIDraftSegment{AssetID: window.AssetID, Start: window.Start, End: window.End}
			if len(parts) > 0 && !planAll && !planOverlap {
				break
			}
			if !planOverlap && (aiSelectedOverlap(append(selected, candidate)) || duration+window.End-window.Start > in.Target+.001) {
				continue
			}
			title, reason := "กาแฟ", "ภาพและคำพูด"
			if distinctPlanRoles {
				title, reason = fmt.Sprintf("ส่วนเรื่อง %d", len(parts)+1), fmt.Sprintf("บทบาทเรื่อง %d", len(parts)+1)
			}
			parts = append(parts, part{window.ID, title, reason})
			selected = append(selected, candidate)
			duration += window.End - window.Start
		}
		return aiJSON(struct {
			Segments       []part `json:"segments"`
			DurationReason string `json:"durationReason,omitempty"`
		}{parts, planReason}), nil
	}
	return nil, editorai.ErrInvalid
}
func aiSetup(t *testing.T, a *aiTestASR, p *aiTestProvider) (*gorm.DB, *VideoEditorFacade, *VideoEditorAIFacade, string, string, string) {
	t.Helper()
	db := aiTestDB(t, true)
	store := newVETestStore(t)
	editor := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), store, videoprocessing.NewFFmpegProcessor(""))
	user := veTestUser(t, db)
	project, _, e := editor.CreateProject(context.Background(), user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "AI test"})
	if e != nil {
		t.Fatal(e)
	}
	asset, _, e := editor.Upload(context.Background(), project.ID, user, uuid.NewString(), "source.mp4", "video/mp4", veFixture(t))
	if e != nil {
		t.Fatal(e)
	}
	f := NewVideoEditorAIFacade(repo.NewVideoEditorAIRepository(db), editor, p, a, EditorAIOptions{Enabled: true, Fixture: true})
	return db, editor, f, user, project.ID, asset.ID
}
func aiRequest(asset string) req.CreateVideoEditorAIJob {
	return req.CreateVideoEditorAIJob{RequestID: uuid.NewString(), Revision: 1, AssetIDs: []string{asset}, Brief: "ทำคลิปกาแฟ", TargetDuration: 1, Style: "warm", AudioMode: "keep", RemoveSilence: true, CloudConsent: true}
}
func TestEditorAILifecycleCacheApprovalAndApplyReplay(t *testing.T) {
	ctx := context.Background()
	asr := &aiTestASR{}
	provider := &aiTestProvider{}
	_, editor, f, user, project, asset := aiSetup(t, asr, provider)
	input := aiRequest(asset)
	j, created, e := f.Create(ctx, project, user, input)
	if e != nil || !created {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	j, e = f.Get(ctx, j.ID, user)
	if e != nil || j.Status != "awaiting_concept" || len(j.Transcripts) != 1 || len(j.Concepts) != 2 || j.CloudCallsUsed != 3 {
		t.Fatal(j, e)
	}
	if _, e = f.Select(ctx, j.ID, user, j.Concepts[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Select(ctx, j.ID, user, j.Concepts[0].ID); e != nil {
		t.Fatal("duplicate selection", e)
	}
	if _, e = f.Select(ctx, j.ID, user, j.Concepts[1].ID); !errors.Is(e, repo.ErrAISelection) {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	j, e = f.Get(ctx, j.ID, user)
	if e != nil || j.Status != "review" || j.Draft == nil || j.CloudCallsUsed != 4 {
		t.Fatal(j, e)
	}
	apply := req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 1, DraftRevision: 1}
	if _, e = f.Apply(ctx, j.ID, user, apply); !errors.Is(e, repo.ErrAIDraft) {
		t.Fatal("approval missing", e)
	}
	for _, s := range j.Draft.Segments {
		apply.ApprovedSegmentIDs = append(apply.ApprovedSegmentIDs, s.ID)
	}
	p, e := f.Apply(ctx, j.ID, user, apply)
	if e != nil || p.Revision != 2 {
		t.Fatal(p, e)
	}
	p, e = editor.SaveProject(ctx, project, user, req.SaveVideoEditorProject{Revision: p.Revision, Title: "later", AspectRatio: "1:1", Segments: p.Segments})
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.Apply(ctx, j.ID, user, apply)
	if e != nil || replay.Revision != 2 || replay.Title == "later" {
		t.Fatal("apply replay", replay, e)
	}
	apply.RequestID = uuid.NewString()
	if _, e = f.Apply(ctx, j.ID, user, apply); !errors.Is(e, repo.ErrAIAlreadyApplied) {
		t.Fatal(e)
	}
	old, _, e := f.Create(ctx, project, user, input)
	if e != nil || old.ID != j.ID {
		t.Fatal("replay before revision", e)
	}
	input.RequestID = uuid.NewString()
	input.Revision = p.Revision
	j2, _, e := f.Create(ctx, project, user, input)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	j2, e = f.Get(ctx, j2.ID, user)
	if e != nil || j2.CloudCallsUsed != 1 || asr.calls.Load() != 1 || len(j2.Transcripts) != 1 || j2.Transcripts[0].Refinement == nil {
		t.Fatal("cache reuse", j2, asr.calls.Load(), e)
	}
}
func TestEditorAIRemoteCancelAndStalePublish(t *testing.T) {
	asr := &aiTestASR{entered: make(chan struct{}), cancelled: make(chan struct{})}
	db, _, f, user, project, asset := aiSetup(t, asr, &aiTestProvider{})
	j, _, e := f.Create(context.Background(), project, user, aiRequest(asset))
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { _, _ = f.ProcessNext(context.Background()); close(done) }()
	select {
	case <-asr.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("ASR not entered")
	}
	other := NewVideoEditorAIFacade(repo.NewVideoEditorAIRepository(db), f.editor, f.provider, f.asr, EditorAIOptions{Enabled: true})
	if _, e = other.Cancel(context.Background(), j.ID, user); e != nil {
		t.Fatal(e)
	}
	select {
	case <-asr.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("remote cancel not propagated")
	}
	<-done
	result, e := f.Get(context.Background(), j.ID, user)
	if e != nil || result.Status != "cancelled" || result.CloudCallsUsed != 0 {
		t.Fatal(result, e)
	}
	var n int64
	db.Model(&models.VideoEditorAICache{}).Count(&n)
	if n != 0 {
		t.Fatal("cancelled cache publish", n)
	}
}
func TestEditorAIQuotaBudgetDraftCASAndOwnership(t *testing.T) {
	ctx := context.Background()
	provider := &aiTestProvider{rate: true}
	db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
	in := aiRequest(asset)
	j, _, e := f.Create(ctx, project, user, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(ctx); e == nil {
		t.Fatal("expected quota")
	}
	j, _ = f.Get(ctx, j.ID, user)
	if j.Status != "failed" || j.CloudCallsUsed != 1 || j.CanRetry {
		t.Fatal(j)
	}
	if _, e = f.Retry(ctx, j.ID, user, uuid.NewString()); !errors.Is(e, repo.ErrAIRetry) {
		t.Fatal(e)
	}
	other := veTestUser(t, db)
	if _, e = f.Get(ctx, j.ID, other); !errors.Is(e, repo.ErrNotFound) {
		t.Fatal(e)
	}
	if e = db.Model(&models.VideoEditorAIJob{}).Where("id=?", j.ID).Updates(map[string]any{"cloud_calls_used": 5, "retry_at": time.Now().Add(-time.Second)}).Error; e != nil {
		t.Fatal(e)
	}
	retry := uuid.NewString()
	if _, e = f.Retry(ctx, j.ID, user, retry); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Retry(ctx, j.ID, user, retry); e != nil {
		t.Fatal("retry replay", e)
	}
	if _, e = f.ProcessNext(ctx); e == nil {
		t.Fatal("expected quota")
	}
	j, _ = f.Get(ctx, j.ID, user)
	if j.CloudCallsUsed != 6 || j.CanRetry {
		t.Fatal(j)
	}
	if _, e = f.Retry(ctx, j.ID, user, uuid.NewString()); !errors.Is(e, repo.ErrAIRetry) {
		t.Fatal(e)
	}
}
func TestEditorAIDefaultOffWithoutSchema(t *testing.T) {
	db := aiTestDB(t, false)
	editor := NewVideoEditorFacade(repo.NewVideoEditorRepository(db), newVETestStore(t), videoprocessing.NewFFmpegProcessor(""))
	user := veTestUser(t, db)
	p, _, e := editor.CreateProject(context.Background(), user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "manual"})
	if e != nil {
		t.Fatal(e)
	}
	f := NewVideoEditorAIFacade(repo.NewVideoEditorAIRepository(db), editor, nil, nil, EditorAIOptions{})
	if f.Capabilities().Available {
		t.Fatal("off")
	}
	list, e := f.List(context.Background(), p.ID, user)
	if e != nil || len(list.Items) != 0 {
		t.Fatal(list, e)
	}
	if _, _, e = f.Create(context.Background(), p.ID, user, aiRequest(uuid.NewString())); !errors.Is(e, editorai.ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = f.Get(context.Background(), uuid.NewString(), user); !errors.Is(e, editorai.ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = f.ProcessNext(context.Background()); e != nil {
		t.Fatal(e)
	}
}

func TestEditorAIConcurrentBudgetAndCancelledFences(t *testing.T) {
	ctx := context.Background()
	db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, &aiTestProvider{})
	job, _, err := f.Create(ctx, project, user, aiRequest(asset))
	if err != nil {
		t.Fatal(err)
	}
	r := repo.NewVideoEditorAIRepository(db)
	claimed, err := r.Claim(ctx)
	if err != nil || claimed.ID.String() != job.ID {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := *claimed
			if r.ReserveCall(ctx, &local) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	stored, err := r.Get(ctx, job.ID, user)
	if err != nil || successes.Load() != 6 || stored.CloudCallsUsed != 6 {
		t.Fatal(successes.Load(), stored, err)
	}
	if _, err = r.Cancel(ctx, job.ID, user); err != nil {
		t.Fatal(err)
	}
	if err = r.ReserveCall(ctx, claimed); !errors.Is(err, repo.ErrAIState) {
		t.Fatal(err)
	}
	if err = r.UpdateClaim(ctx, claimed, map[string]any{"status": "review"}); !errors.Is(err, repo.ErrAIState) {
		t.Fatal(err)
	}
	if err = r.PublishCache(ctx, claimed, &models.VideoEditorAICache{}); !errors.Is(err, repo.ErrAIState) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.VideoEditorAICache{}).Count(&count)
	if count != 0 {
		t.Fatal("stale cache published")
	}
}

func TestEditorAIGeneratedShortPlanAndLegacyApplyAllowed(t *testing.T) {
	ctx := context.Background()
	provider := &aiTestProvider{}
	db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
	input := aiRequest(asset)
	input.TargetDuration = 60
	job, _, err := f.Create(ctx, project, user, input)
	if err != nil {
		t.Fatal(err)
	}
	var stored models.VideoEditorAIJob
	if err = db.First(&stored, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := aiInput(&stored)
	snapshot.Sources[0].Duration = 60
	concept := models.EditorAIConcept{ID: uuid.NewString(), Title: "sixty", Summary: "sixty", Duration: 60}
	evidence := []models.EditorAIEvidence{}
	for start := 0.0; start < 60; start += 15 {
		evidence = append(evidence, models.EditorAIEvidence{ID: uuid.NewString(), AssetID: asset, Start: start, End: start + 15, Description: "fixture"})
	}
	if err = db.Model(&stored).Updates(map[string]any{"input": aiJSON(snapshot), "phase": "planning", "concepts": aiJSON([]models.EditorAIConcept{concept}), "selected_concept_id": concept.ID, "evidence": aiJSON(evidence), "visuals": aiJSON([]models.EditorAIVisual{}), "transcripts": aiJSON([]models.EditorAITranscript{})}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, job.ID, user)
	if err != nil || got.Status != "review" || got.Draft == nil || got.Draft.Duration != 15 || got.CloudCallsUsed != 1 || provider.calls != 1 {
		t.Fatalf("job=%+v err=%v calls=%d", got, err, provider.calls)
	}
	// Legacy review drafts can be applied without a manual save solely to confirm a short duration.
	segment := models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 1, Volume: 1, Title: "manual"}
	draft := models.EditorAIDraft{Revision: 1, Duration: 1, Segments: []models.EditorAIDraftSegment{segment}}
	if err = db.Model(&stored).Updates(map[string]any{"status": "review", "stage": "review", "draft": aiJSON(draft)}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = f.Apply(ctx, job.ID, user, req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 1, DraftRevision: 1, ApprovedSegmentIDs: []string{segment.ID}})
	if err != nil {
		t.Fatalf("legacy short draft apply: %v", err)
	}
}

func TestEditorAIRefinedSpeechReachesConceptsAndPlanningAllModes(t *testing.T) {
	for _, removeSilence := range []bool{false, true} {
		t.Run(fmt.Sprint(removeSilence), func(t *testing.T) {
			ctx := context.Background()
			provider := &aiTestProvider{refinedText: "กาแฟ."}
			db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
			input := aiRequest(asset)
			input.RemoveSilence = removeSilence
			j, _, err := f.Create(ctx, project, user, input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.ProcessNext(ctx); err != nil {
				t.Fatal(err)
			}
			j, err = f.Get(ctx, j.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			if len(j.Transcripts) != 1 || j.Transcripts[0].Text != "กาแฟ" || j.Transcripts[0].Refinement == nil || j.Transcripts[0].Refinement.Text != "กาแฟ." || j.Transcripts[0].Segments[0].Text != "กาแฟ" {
				t.Fatalf("raw/refined separation: %+v", j.Transcripts)
			}
			var rawCache models.VideoEditorAICache
			if err = db.Where("asset_id=? AND signature=?", asset, f.asr.Signature()).First(&rawCache).Error; err != nil {
				t.Fatal(err)
			}
			var raw models.EditorAITranscript
			if json.Unmarshal(rawCache.Result, &raw) != nil || raw.Refinement != nil || raw.Text != "กาแฟ" {
				t.Fatal("raw cache contaminated")
			}
			if _, err = f.Select(ctx, j.ID, user, j.Concepts[0].ID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.ProcessNext(ctx); err != nil {
				t.Fatal(err)
			}
			provider.mu.Lock()
			prompts := append([]string(nil), provider.prompts...)
			provider.mu.Unlock()
			seen := map[string]bool{}
			for _, prompt := range prompts {
				var body struct {
					Task        string              `json:"task"`
					Transcripts []compactTranscript `json:"transcripts"`
				}
				if json.Unmarshal([]byte(prompt), &body) != nil {
					t.Fatal("bad prompt")
				}
				if body.Task == "concepts" || body.Task == "planning" {
					seen[body.Task] = true
					if len(body.Transcripts) != 1 || body.Transcripts[0].Text != "กาแฟ." {
						t.Fatalf("%s lost refined source: %s", body.Task, prompt)
					}
				}
			}
			if !seen["concepts"] || !seen["planning"] {
				t.Fatal("missing downstream stage")
			}
		})
	}
}

func TestEditorAITranscriptPublishedBeforeLaterStages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	refining, visual := make(chan struct{}), make(chan struct{})
	releaseRefining, releaseVisual := make(chan struct{}), make(chan struct{})
	provider := &aiTestProvider{refinedText: "กาแฟ.", onTask: func(ctx context.Context, task string) {
		var wait <-chan struct{}
		if task == "refine_transcripts" {
			close(refining)
			wait = releaseRefining
		}
		if task == "vision" {
			close(visual)
			wait = releaseVisual
		}
		if wait != nil {
			select {
			case <-wait:
			case <-ctx.Done():
			}
		}
	}}
	_, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
	j, _, err := f.Create(ctx, project, user, aiRequest(asset))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.ProcessNext(ctx); done <- err }()
	select {
	case <-refining:
	case <-ctx.Done():
		t.Fatal("refinement not reached")
	}
	raw, err := f.Get(ctx, j.ID, user)
	if err != nil || raw.Stage != "transcribing" || len(raw.Transcripts) != 1 || raw.Transcripts[0].Refinement != nil || raw.Transcripts[0].Text != "กาแฟ" {
		t.Fatalf("raw not published while refining: %+v %v", raw, err)
	}
	close(releaseRefining)
	select {
	case <-visual:
	case <-ctx.Done():
		t.Fatal("visual not reached")
	}
	refined, err := f.Get(ctx, j.ID, user)
	if err != nil || refined.Stage != "visual_analysis" || len(refined.Concepts) != 0 || len(refined.Transcripts) != 1 || refined.Transcripts[0].Refinement == nil || refined.Transcripts[0].Refinement.Text != "กาแฟ." {
		t.Fatalf("refined transcript delayed until concepts: %+v %v", refined, err)
	}
	if !reflect.DeepEqual(raw.Transcripts[0].Segments, refined.Transcripts[0].Segments) {
		t.Fatal("raw times or words changed")
	}
	close(releaseVisual)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestEditorAIRefinementSkipsSilenceAndRejectsMalformedResponse(t *testing.T) {
	for _, silent := range []bool{false, true} {
		t.Run(fmt.Sprint(silent), func(t *testing.T) {
			ctx := context.Background()
			provider := &aiTestProvider{malformedRefinement: !silent}
			_, _, f, user, project, asset := aiSetup(t, &aiTestASR{silent: silent}, provider)
			j, _, err := f.Create(ctx, project, user, aiRequest(asset))
			if err != nil {
				t.Fatal(err)
			}
			_, processErr := f.ProcessNext(ctx)
			j, err = f.Get(ctx, j.ID, user)
			if err != nil {
				t.Fatal(err)
			}
			if silent {
				if processErr != nil || j.Status != "awaiting_concept" || j.CloudCallsUsed != 2 || j.Transcripts[0].HasSpeech || j.Transcripts[0].Refinement != nil {
					t.Fatalf("silence invented speech/call: %+v %v", j, processErr)
				}
			} else {
				if processErr == nil || j.Status != "failed" || j.FailureCode != "AI_INVALID_RESULT" || j.CloudCallsUsed != 1 || j.Transcripts[0].Refinement != nil {
					t.Fatalf("malformed refinement accepted: %+v %v", j, processErr)
				}
			}
		})
	}
}

func TestEditorAICoherentTargetConceptAndLongShot(t *testing.T) {
	ctx := context.Background()
	provider := &aiTestProvider{conceptDuration: 30, planAll: true}
	_, editor, f, user, project, _ := aiSetup(t, &aiTestASR{}, provider)
	path := filepath.Join(t.TempDir(), "long.mp4")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x120:r=1:d=120", "-f", "lavfi", "-i", "sine=frequency=440:duration=120", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads", "1", "-y", path).CombinedOutput(); e != nil {
		t.Fatalf("long fixture: %v %s", e, out)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	assetRow, _, e := editor.Upload(ctx, project, user, uuid.NewString(), "long.mp4", "video/mp4", data)
	if e != nil {
		t.Fatal(e)
	}
	asset := assetRow.ID
	input := aiRequest(asset)
	input.TargetDuration = 60
	job, _, err := f.Create(ctx, project, user, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, job.ID, user)
	if err != nil || got.Status != "awaiting_concept" || got.TargetDuration != 60 || got.Concepts[0].Duration != 60 || got.Concepts[0].DurationReason != "" {
		t.Fatalf("target lost: %+v %v", got, err)
	}
	if _, err = f.Select(ctx, job.ID, user, got.Concepts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = f.Get(ctx, job.ID, user)
	if err != nil || got.Status != "review" || got.Draft == nil || got.Draft.Duration != 60 || len(got.Draft.Segments) != 1 || got.Draft.Segments[0].End-got.Draft.Segments[0].Start <= 6 {
		t.Fatalf("long continuous target failed: %+v %v", got, err)
	}
}

func TestEditorAIAdjacentWindowsMergeAndShortReasonNonblocking(t *testing.T) {
	ctx := context.Background()
	provider := &aiTestProvider{planAll: true}
	db, _, f, user, project, asset := aiSetup(t, &aiTestASR{}, provider)
	input := aiRequest(asset)
	input.TargetDuration = 60
	job, _, err := f.Create(ctx, project, user, input)
	if err != nil {
		t.Fatal(err)
	}
	var stored models.VideoEditorAIJob
	if err = db.First(&stored, "id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := aiInput(&stored)
	snapshot.Sources[0].Duration = 120
	concept := models.EditorAIConcept{ID: uuid.NewString(), Title: "short", Summary: "short", Duration: 30, DurationReason: "คำตอบช่วงนี้จบใน30วินาที"}
	evidence := []models.EditorAIEvidence{{ID: uuid.NewString(), AssetID: asset, Start: 0, End: 15}, {ID: uuid.NewString(), AssetID: asset, Start: 15, End: 30}}
	if err = db.Model(&stored).Updates(map[string]any{"input": aiJSON(snapshot), "phase": "planning", "concepts": aiJSON([]models.EditorAIConcept{concept}), "selected_concept_id": concept.ID, "evidence": aiJSON(evidence), "visuals": aiJSON([]models.EditorAIVisual{}), "transcripts": aiJSON([]models.EditorAITranscript{})}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, job.ID, user)
	if err != nil || got.Status != "review" || got.Draft == nil || got.Draft.Duration != 30 || got.Draft.DurationReason == "" || len(got.Draft.Segments) != 1 {
		t.Fatalf("nonblocking short merge: %+v %v", got, err)
	}
	if err = db.First(&stored, "id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	var mergedEvidence []models.EditorAIEvidence
	if json.Unmarshal(stored.Evidence, &mergedEvidence) != nil {
		t.Fatal("invalid evidence")
	}
	found := false
	for _, e := range mergedEvidence {
		if e.ID == got.Draft.Segments[0].EvidenceID && e.Start == 0 && e.End == 30 {
			found = true
		}
	}
	if !found {
		t.Fatal("merged draft lost exact evidence provenance")
	}
	// Legacy-compatible short drafts remain applicable; use actual owned fixture bounds.
	segment := models.EditorAIDraftSegment{ID: uuid.NewString(), AssetID: asset, End: 1, Volume: 1, Title: "short"}
	draft := models.EditorAIDraft{Revision: 1, Duration: 1, DurationReason: got.Draft.DurationReason, Segments: []models.EditorAIDraftSegment{segment}}
	if err = db.Model(&stored).Update("draft", aiJSON(draft)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.Apply(ctx, job.ID, user, req.ApplyVideoEditorAIDraft{RequestID: uuid.NewString(), Revision: 1, DraftRevision: 1, ApprovedSegmentIDs: []string{segment.ID}}); err != nil {
		t.Fatalf("shortfall blocked apply: %v", err)
	}
}
