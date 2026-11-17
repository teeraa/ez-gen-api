package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

type extensionTestProcessor struct {
	*testLastFrameProcessor
	tails, validations         int
	tailFailure, outputFailure error
}

func (p *extensionTestProcessor) PrepareContinuation(context.Context, []byte) ([]byte, error) {
	p.tails++
	if p.tailFailure != nil {
		return nil, p.tailFailure
	}
	return []byte("three-second-tail"), nil
}
func (p *extensionTestProcessor) ValidateContinuationOutput(context.Context, []byte, float64) error {
	p.validations++
	return p.outputFailure
}
func extensionFixture(t *testing.T, model string) (*DirectorFacade, *syntheticTestStore, *modelark.MockClient, *continuityMedia, *extensionTestProcessor, *models.DirectorJob, directorJobInput) {
	t.Helper()
	f, s, v, m, j, in := newContinuityFixture(t)
	store := &syntheticTestStore{continuityStore: s}
	f.store = store
	d := continuityDoc(t, s)
	d.Project.Style = "anime"
	d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
	d.Keys["video:a"] = "prior-owned.mp4"
	d.VideoFrames["a"] = directorVideoFrame{Key: "prior-last.png", ClipKey: "prior-owned.mp4"}
	_ = directorEncode(&s.row, &d)
	in.Project = d.Project
	in.ShotID = "b"
	in.ImageKey = "b.png"
	in.Generation = "gb"
	in.VideoModel = model
	j.TargetID = "video:b"
	processor := &extensionTestProcessor{testLastFrameProcessor: &testLastFrameProcessor{}}
	f.options.Processor = processor
	f.options.VideoFetch = func(_ context.Context, u string) ([]byte, string, error) {
		if !strings.HasPrefix(u, "https://owned.test/") {
			t.Fatal("unowned video fetch")
		}
		return []byte("previous-or-generated-owned-video"), "video/mp4", nil
	}
	return f, store, v, m, processor, j, in
}
func TestDirectorExtensionUsesOneVideoTailAcrossSupportedModels(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"dreamina-seedance-2-0-fast-260128", "dreamina-seedance-2-0-260128", "dreamina-seedance-2-5-260628"} {
		t.Run(model, func(t *testing.T) {
			f, s, v, m, p, j, in := extensionFixture(t, model)
			if model == "dreamina-seedance-2-5-260628" {
				in.Draft = true
			}
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if len(v.CreatedRequests) != 1 || p.tails != 1 {
				t.Fatal("extension not submitted once")
			}
			r := v.CreatedRequests[0]
			if len(r.Content) != 2 || r.Content[1].Type != modelark.ContentTypeVideoURL || r.Content[1].Role != modelark.RoleReferenceVideo || !strings.HasSuffix(r.Content[1].URL, "-tail.mp4") || r.DurationSeconds != 5 || !strings.Contains(r.Content[0].Text, "Extend Video1 forward") || !strings.Contains(r.Content[0].Text, "ONLY new footage") {
				t.Fatalf("wrong extension request %+v", r)
			}
			if model == "dreamina-seedance-2-5-260628" && (r.Ratio != "adaptive" || !r.Draft || r.Resolution != "480p") {
				t.Fatalf("2.5 constraints lost %+v", r)
			}
			var task directorVideoTask
			_ = json.Unmarshal(j.Output, &task)
			if !task.Extension || task.SourceClipKey != "prior-owned.mp4" || task.FirstImageKey != "prior-last.png" {
				t.Fatalf("extension lineage missing %+v", task)
			}
			tailCount := 0
			for k := range m.objects {
				if strings.HasSuffix(k, "-tail.mp4") {
					tailCount++
				}
			}
			if tailCount != 1 {
				t.Fatal("tail was not durably stored")
			}
			v.SetTask(v.NextTaskID, &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/new.mp4"})
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if j.Status != "succeeded" || p.validations != 1 || p.calls != 1 || len(v.CreatedRequests) != 1 || continuityDoc(t, s.continuityStore).VideoFrames["b"].SourceClipKey != "prior-owned.mp4" {
				t.Fatal("extension output not finalized safely")
			}
		})
	}
}
func TestDirectorExtensionUncertainSubmissionNeverRepeatsPaidTask(t *testing.T) {
	t.Parallel()
	f, s, v, _, p, j, in := extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
	s.failWait = true
	if e := f.directorVideo(context.Background(), j, in); e == nil {
		t.Fatal("expected wait failure")
	}
	s.failWait = false
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 1 || p.tails != 1 {
		t.Fatal("accepted extension submitted or prepared again")
	}
}
func TestDirectorExtensionRejectsChangedSourceAndCombinedOutput(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"source", "output"} {
		t.Run(kind, func(t *testing.T) {
			f, s, v, m, p, j, in := extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
			if kind == "source" {
				m.onSign = func(key string) {
					if strings.HasSuffix(key, "-tail.mp4") {
						d := continuityDoc(t, s.continuityStore)
						d.Keys["video:a"] = "replacement.mp4"
						_ = directorEncode(&s.row, &d)
					}
				}
			} else {
				p.outputFailure = errors.New("8 second combined output")
			}
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if kind == "source" {
				if len(v.CreatedRequests) != 0 || s.refunds != 1 {
					t.Fatal("changed source incurred provider work")
				}
				return
			}
			v.SetTask(v.NextTaskID, &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/combined.mp4"})
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if j.Status != "needs_attention" || s.refunds != 0 || len(v.CreatedRequests) != 1 {
				t.Fatal("combined output silently trimmed or regenerated")
			}
		})
	}
}

func TestDirectorExtensionPreparationRetriesBeforePaidSubmission(t *testing.T) {
	t.Parallel()
	f, s, v, _, p, j, in := extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
	p.tailFailure = errors.New("temporary tail processing error")
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 0 || s.waits != 1 {
		t.Fatal("paid submission happened before tail ready")
	}
	p.tailFailure = nil
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 1 || p.tails != 2 {
		t.Fatal("local tail retry did not resume safely")
	}
}
func TestDirectorExtensionReadyRestartWithoutProviderWaits(t *testing.T) {
	t.Parallel()
	f, s, v, _, _, j, in := extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
	j.Output, _ = json.Marshal(directorExtensionState{Phase: "ready"})
	before := string(j.Output)
	f.video = nil
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if string(j.Output) != before || s.waits != 1 || len(v.CreatedRequests) != 0 {
		t.Fatal("ready checkpoint changed or submitted without provider")
	}
}
func TestDirectorExtensionSubmittingCrashNeverRepeats(t *testing.T) {
	t.Parallel()
	f, s, v, _, _, j, in := extensionFixture(t, "dreamina-seedance-2-0-fast-260128")
	j.Output, _ = json.Marshal(directorExtensionState{Phase: "submitting"})
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if j.Status != "needs_attention" || len(v.CreatedRequests) != 0 || s.refunds != 0 {
		t.Fatal("uncertain paid submission repeated or refunded")
	}
}

func TestDirectorExtensionReferenceVideoReachesSDKHTTPJSON(t *testing.T) {
	t.Parallel()
	type wire struct {
		Ratio    string `json:"ratio"`
		Duration int    `json:"duration"`
		Content  []struct {
			Type     string `json:"type"`
			Role     string `json:"role"`
			VideoURL struct {
				URL string `json:"url"`
			} `json:"video_url"`
		} `json:"content"`
	}
	got := make(chan wire, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value wire
		if e := json.NewDecoder(r.Body).Decode(&value); e != nil {
			w.WriteHeader(400)
			return
		}
		got <- value
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture-extension"}`))
	}))
	defer server.Close()
	f, _, _, _, _, j, in := extensionFixture(t, "dreamina-seedance-2-5-260628")
	f.video = modelark.NewClient("fixture-only", server.URL+"/api/v3")
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	select {
	case request := <-got:
		if request.Ratio != "adaptive" || request.Duration != 5 || len(request.Content) != 2 || request.Content[1].Type != "video_url" || request.Content[1].Role != "reference_video" || !strings.HasSuffix(request.Content[1].VideoURL.URL, "-tail.mp4") {
			t.Fatalf("wrong SDK extension request %+v", request)
		}
	default:
		t.Fatal("SDK request was not sent")
	}
}
func TestDirectorExtensionDraftFinalizeRetainsValidationAndNoNewTail(t *testing.T) {
	t.Parallel()
	f, s, v, _, p, j, _ := extensionFixture(t, "dreamina-seedance-2-5-260628")
	d := continuityDoc(t, s.continuityStore)
	f.options.Enabled = true
	f.options.AnalysisModel = "fixture"
	f.options.ImageModel = "fixture"
	f.options.StoryPoints = 1
	f.options.ImagePoints = 1
	f.options.DraftPoints = 8
	d.Project.Storyboard.VideoModelID = "seedance-2-5"
	d.Project.Scenes[0].Shots[1].Video.Status = "succeeded"
	d.Project.Scenes[0].Shots[1].Video.Draft = true
	d.VideoDrafts["b"] = directorVideoDraft{TaskID: "extension-draft", Provider: "dreamina-seedance-2-5-260628", CreatedAt: time.Now(), Extension: true}
	d.VideoFrames["b"] = directorVideoFrame{SourceClipKey: "prior-owned.mp4"}
	ids := []string{"b"}
	request := req.DirectorRequest{ShotIDs: &ids, Finalize: true}
	var snapshot directorJobInput
	if e := f.enqueueVideos(&d, request, func(kind, target string, cost int) error {
		job, e := f.job(&d, kind, target, cost, request, func(string) (*models.LibraryAsset, error) { return nil, errors.New("unexpected asset") })
		if e != nil {
			return e
		}
		*j = job
		return json.Unmarshal(job.Snapshot, &snapshot)
	}); e != nil {
		t.Fatal(e)
	}
	if !snapshot.Extension {
		t.Fatal("extension flag not inherited from accepted draft")
	}
	_ = directorEncode(&s.row, &d)
	if e := f.directorVideo(context.Background(), j, snapshot); e != nil {
		t.Fatal(e)
	}
	r := v.CreatedRequests[0]
	if len(r.Content) != 1 || r.Content[0].DraftTaskID != "extension-draft" || r.Ratio != "" || r.DurationSeconds != 0 || p.tails != 0 {
		t.Fatalf("refinement prepared new extension input %+v", r)
	}
	var task directorVideoTask
	_ = json.Unmarshal(j.Output, &task)
	if !task.Extension {
		t.Fatal("refinement lost extension output-validation marker")
	}
}
