package facade_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

type continuityStore struct {
	directorStore
	row            models.DirectorProject
	waits, refunds int
}

func (s *continuityStore) Get(context.Context, string, string) (*models.DirectorProject, error) {
	return &s.row, nil
}
func (s *continuityStore) Progress(_ context.Context, _ *models.DirectorJob, fn func(*models.DirectorProject) error) error {
	return fn(&s.row)
}
func (s *continuityStore) Complete(_ context.Context, j *models.DirectorJob, status string, refund bool, fn func(*models.DirectorProject) error) error {
	if err := fn(&s.row); err != nil {
		return err
	}
	j.Status = status
	if refund {
		s.refunds++
	}
	return nil
}
func (s *continuityStore) Wait(_ context.Context, j *models.DirectorJob, out []byte, _ time.Time) error {
	s.waits++
	j.Output = out
	j.Status = "waiting"
	return nil
}

func (s *continuityStore) RecordOutput(_ context.Context, j *models.DirectorJob, out []byte) error {
	j.Output = append([]byte(nil), out...)
	return nil
}

type continuityMedia struct {
	objects   map[string][]byte
	failFrame bool
	onSign    func(string)
}

func (m *continuityMedia) SignedURL(_ context.Context, key string, _ time.Duration) (string, time.Time, error) {
	if m.onSign != nil {
		m.onSign(key)
	}
	return "https://owned.test/" + key, time.Now().Add(time.Hour), nil
}
func (m *continuityMedia) Upload(_ context.Context, key string, b []byte, _ string) error {
	if m.failFrame && strings.Contains(key, "-last.") {
		return errors.New("storage temporarily unavailable")
	}
	m.objects[key] = b
	return nil
}
func (m *continuityMedia) Fetch(_ context.Context, u string) ([]byte, string, error) {
	if strings.HasSuffix(u, ".png") {
		var b bytes.Buffer
		_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
		return b.Bytes(), "image/png", nil
	}
	return []byte("video"), "video/mp4", nil
}
func newContinuityFixture(t *testing.T) (*DirectorFacade, *continuityStore, *modelark.MockClient, *continuityMedia, *models.DirectorJob, directorJobInput) {
	t.Helper()
	p := directorTestProject()
	p.AspectRatio = "16:9"
	p.Resolution = "720p"
	p.Storyboard.Status = "succeeded"
	p.Storyboard.VideoModelID = "seedance-2-0-fast"
	for i := range p.Scenes[0].Shots {
		p.Scenes[0].Shots[i].Image.Status = "succeeded"
		p.Scenes[0].Shots[i].Video = &dto.DirectorVideo{Status: "queued"}
	}
	d := directorDocument{Project: p, Keys: map[string]string{"a": "a.png", "b": "b.png"}, Generations: map[string]string{"video:a": "ga", "video:b": "gb"}, VideoFrames: map[string]directorVideoFrame{}}
	s := &continuityStore{}
	if err := directorEncode(&s.row, &d); err != nil {
		t.Fatal(err)
	}
	v := modelark.NewMockClient()
	m := &continuityMedia{objects: map[string][]byte{}}
	f := NewDirectorFacade(s, nil, m, nil, nil, nil, v, m, DirectorOptions{VideoEnabled: true, VideoPoints: 20, Processor: &testLastFrameProcessor{}})
	j := &models.DirectorJob{ProjectID: uuid.New(), UserID: uuid.New(), Kind: "video", TargetID: "video:a", Cost: 20}
	j.ID = uuid.New()
	in := directorJobInput{ShotID: "a", ImageKey: "a.png", VideoModel: "fixture", Ratio: "16:9", Resolution: "720p", Seconds: 5, Generation: "ga"}
	return f, s, v, m, j, in
}
func continuityDoc(t *testing.T, s *continuityStore) directorDocument {
	t.Helper()
	d, e := directorDecode(&s.row)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestDirectorContinuityWaitsAndUsesDurableActualFrame(t *testing.T) {
	f, s, v, m, j, in := newContinuityFixture(t)
	c := context.Background()
	next := *j
	next.ID = uuid.New()
	next.TargetID = "video:b"
	nextIn := in
	nextIn.ShotID = "b"
	nextIn.ImageKey = "b.png"
	nextIn.Generation = "gb"
	if e := f.directorVideo(c, &next, nextIn); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 0 || s.waits != 1 {
		t.Fatal("dependent clip submitted before predecessor")
	}
	if e := f.directorVideo(c, j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 1 || !v.CreatedRequests[0].ReturnLastFrame || len(v.CreatedRequests[0].Content) != 2 {
		t.Fatalf("first request: %+v", v.CreatedRequests)
	}
	v.SetTask(v.NextTaskID, &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/a.mp4", LastFrameURL: "https://provider.test/final.png"})
	m.failFrame = true
	if e := f.directorVideo(c, j, in); e != nil {
		t.Fatal(e)
	}
	if continuityDoc(t, s).Project.Scenes[0].Shots[0].Video.Status == "succeeded" {
		t.Fatal("published without durable frame")
	}
	m.failFrame = false
	if e := f.directorVideo(c, j, in); e != nil {
		t.Fatal(e)
	}
	d := continuityDoc(t, s)
	frame := d.VideoFrames["a"]
	if frame.Key == "" || len(m.objects[frame.Key]) == 0 {
		t.Fatal("frame not stored")
	}
	if e := f.directorVideo(c, &next, nextIn); e != nil {
		t.Fatal(e)
	}
	r := v.CreatedRequests[1]
	if r.Content[1].URL != "https://owned.test/"+frame.Key || len(r.Content) != 2 || !strings.Contains(r.Content[0].Text, "actual final frame") {
		t.Fatalf("wrong continuity source: %+v", r)
	}
	if e := f.directorVideo(c, &next, nextIn); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 2 {
		t.Fatal("recovery resubmitted paid task")
	}
	d = continuityDoc(t, s)
	d.Project.Scenes[0].Shots[0].Video.Status = "queued"
	_ = directorEncode(&s.row, &d)
	if e := f.directorVideo(c, &next, nextIn); e != nil {
		t.Fatal(e)
	}
	if !continuityDoc(t, s).Project.Scenes[0].Shots[1].Video.Stale {
		t.Fatal("source regeneration did not invalidate dependent result")
	}
}
func TestDirectorContinuityMissingOrFailedSourceNeverSubmits(t *testing.T) {
	for _, status := range []string{"failed", "needs_attention", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			f, s, v, _, j, in := newContinuityFixture(t)
			d := continuityDoc(t, s)
			d.Project.Scenes[0].Shots[0].Video.Status = status
			_ = directorEncode(&s.row, &d)
			j.TargetID = "video:b"
			in.ShotID = "b"
			in.Generation = "gb"
			in.ImageKey = "b.png"
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if len(v.CreatedRequests) != 0 || s.refunds != 1 || j.Status != "failed" {
				t.Fatalf("dependency failure submitted or not refunded: %+v", j)
			}
		})
	}
}
func TestDirectorContinuityUnrecoverableFrameHasFiniteRetries(t *testing.T) {
	f, s, v, _, j, in := newContinuityFixture(t)
	f.options.Processor = nil
	c := context.Background()
	if e := f.directorVideo(c, j, in); e != nil {
		t.Fatal(e)
	}
	v.SetTask(v.NextTaskID, &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/a.mp4"})
	for attempt := 0; attempt < 3; attempt++ {
		if e := f.directorVideo(c, j, in); e != nil {
			t.Fatal(e)
		}
	}
	d := continuityDoc(t, s)
	sh := d.Project.Scenes[0].Shots[0]
	if j.Status != "needs_attention" || sh.Video.FailureCode == nil || *sh.Video.FailureCode != "VIDEO_FRAME_RECOVERY_FAILED" || len(d.VideoFrames) != 0 || s.refunds != 0 {
		t.Fatalf("frame retries were not bounded: %+v", sh.Video)
	}
}

func TestDirectorContinuityStartsEachSceneFromOwnImage(t *testing.T) {
	f, s, v, _, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	d.Project.Scenes = append(d.Project.Scenes, dto.DirectorScene{ID: "s2", Shots: []dto.DirectorShot{{ID: "c", SceneID: "s2", Video: &dto.DirectorVideo{Status: "queued"}}}})
	d.Generations["video:c"] = "gc"
	_ = directorEncode(&s.row, &d)
	j.TargetID = "video:c"
	in.ShotID = "c"
	in.Generation = "gc"
	in.ImageKey = "c.png"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if v.CreatedRequests[0].Content[1].URL != "https://owned.test/c.png" {
		t.Fatal("cross-scene source leaked")
	}
}
func TestDirectorContinuityRefinementKeepsDraftSource(t *testing.T) {
	f, s, v, _, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	d.VideoFrames["a"] = directorVideoFrame{SourceClipKey: "original-source"}
	_ = directorEncode(&s.row, &d)
	in.DraftTaskID = "draft-task"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	r := v.CreatedRequests[0]
	var task directorVideoTask
	_ = json.Unmarshal(j.Output, &task)
	if len(r.Content) != 1 || r.Content[0].DraftTaskID != "draft-task" || !r.ReturnLastFrame || task.SourceClipKey != "original-source" {
		t.Fatalf("draft lineage lost: %+v %+v", r, task)
	}
}

func TestDirectorContinuityAdmissionOrderAndLegacyRejection(t *testing.T) {
	f, s, _, _, _, _ := newContinuityFixture(t)
	f.options.Enabled = true
	f.options.AnalysisModel = "fixture"
	f.options.ImageModel = "fixture"
	f.options.StoryPoints = 1
	f.options.ImagePoints = 1
	d := continuityDoc(t, s)
	for k := range d.Project.Scenes[0].Shots {
		d.Project.Scenes[0].Shots[k].Video = nil
	}
	ids := []string{"b"}
	request := req.DirectorRequest{ShotIDs: &ids}
	if err := f.enqueueVideos(&d, request, func(string, string, int) error { return nil }); err == nil {
		t.Fatal("legacy or missing predecessor accepted")
	}
	ids = []string{"b", "a"}
	got := []string{}
	if err := f.enqueueVideos(&d, request, func(_ string, id string, _ int) error { got = append(got, id); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "video:a,video:b" || d.Project.Scenes[0].Shots[0].Video.UsesLastFrame || !d.Project.Scenes[0].Shots[1].Video.UsesLastFrame {
		t.Fatalf("wrong dependency order: %v", got)
	}
}

func TestDirectorContinuityChangedBeforeSubmissionDoesNotSpend(t *testing.T) {
	f, s, v, m, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
	d.Keys["video:a"] = "old.mp4"
	d.VideoFrames["a"] = directorVideoFrame{Key: "frame.png", ClipKey: "old.mp4"}
	_ = directorEncode(&s.row, &d)
	m.onSign = func(string) {
		changed := continuityDoc(t, s)
		changed.Keys["video:a"] = "replacement.mp4"
		_ = directorEncode(&s.row, &changed)
	}
	j.TargetID = "video:b"
	in.ShotID = "b"
	in.Generation = "gb"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(v.CreatedRequests) != 0 || s.refunds != 1 {
		t.Fatal("source mutation caused paid submission")
	}
}
func TestDirectorContinuityRegenerationInvalidatesSuccessfulFollowers(t *testing.T) {
	f, s, _, _, _, _ := newContinuityFixture(t)
	f.options.Enabled = true
	f.options.AnalysisModel = "fixture"
	f.options.ImageModel = "fixture"
	f.options.StoryPoints = 1
	f.options.ImagePoints = 1
	d := continuityDoc(t, s)
	for k := range d.Project.Scenes[0].Shots {
		d.Project.Scenes[0].Shots[k].Video.Status = "succeeded"
	}
	ids := []string{"a"}
	if err := f.enqueueVideos(&d, req.DirectorRequest{ShotIDs: &ids}, func(string, string, int) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !d.Project.Scenes[0].Shots[1].Video.Stale {
		t.Fatal("dependent successful video stayed current")
	}
}

func TestDirectorContinuityStaleClipIsSkippedByExport(t *testing.T) {
	f, s, _, _, _, _ := newContinuityFixture(t)
	d := continuityDoc(t, s)
	for k := range d.Project.Scenes[0].Shots {
		sh := &d.Project.Scenes[0].Shots[k]
		sh.Video.Status = "succeeded"
		d.Keys[directorVideoTarget(sh.ID)] = sh.ID + ".mp4"
	}
	d.Project.Export = &dto.DirectorExport{Status: "succeeded"}
	d.ExportSource = storyHash(directorClipKeys(&d))
	d.Project.Scenes[0].Shots[1].Video.Stale = true
	stale := d.Keys[directorVideoTarget(d.Project.Scenes[0].Shots[1].ID)]
	keys := directorClipKeys(&d)
	if len(keys) != len(d.Project.Scenes[0].Shots)-1 || slices.Contains(keys, stale) {
		t.Fatalf("export should skip only the stale clip: %v", keys)
	}
	_ = directorEncode(&s.row, &d)
	p, e := f.present(context.Background(), &s.row)
	if e != nil {
		t.Fatal(e)
	}
	if !p.Export.Stale {
		t.Fatal("existing export stays current after clip invalidation")
	}
}
func TestDirectorContinuityFinalizeRejectsChangingDraftLineageBeforeCharge(t *testing.T) {
	for _, kind := range []string{"batch", "changed", "legacy", "valid"} {
		t.Run(kind, func(t *testing.T) {
			f, s, _, _, _, _ := newContinuityFixture(t)
			f.options.Enabled = true
			f.options.AnalysisModel = "fixture"
			f.options.ImageModel = "fixture"
			f.options.StoryPoints = 1
			f.options.ImagePoints = 1
			f.options.DraftPoints = 8
			d := continuityDoc(t, s)
			d.Project.Storyboard.VideoModelID = "seedance-2-5"
			for k := range d.Project.Scenes[0].Shots {
				sh := &d.Project.Scenes[0].Shots[k]
				sh.Video.Status = "succeeded"
				sh.Video.Draft = true
				d.VideoDrafts[sh.ID] = directorVideoDraft{TaskID: "draft-" + sh.ID, CreatedAt: time.Now()}
				d.Keys[directorVideoTarget(sh.ID)] = sh.ID + ".mp4"
			}
			d.VideoFrames["b"] = directorVideoFrame{SourceClipKey: "a.mp4"}
			ids := []string{"b"}
			if kind == "batch" {
				ids = []string{"a", "b"}
			}
			if kind == "changed" {
				d.Keys["video:a"] = "new.mp4"
			}
			if kind == "legacy" {
				delete(d.VideoFrames, "b")
			}
			enqueued := 0
			e := f.enqueueVideos(&d, req.DirectorRequest{ShotIDs: &ids, Finalize: true}, func(string, string, int) error { enqueued++; return nil })
			if kind == "valid" {
				if e != nil || enqueued != 1 {
					t.Fatalf("valid refinement rejected %v", e)
				}
			} else if e == nil || enqueued != 0 {
				t.Fatalf("incompatible draft charged: err=%v queued=%d", e, enqueued)
			}
		})
	}
}

func TestDirectorContinuityFilmedStylesContinueFromPreviousLastFrame(t *testing.T) {
	t.Parallel()
	for _, style := range []string{"cinematic", "realistic"} {
		t.Run(style, func(t *testing.T) {
			f, s, v, images, _, j, in := syntheticFixture(t)
			d := continuityDoc(t, s.continuityStore)
			d.Project.Style = style
			d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
			d.Keys["video:a"] = "a.mp4"
			d.VideoFrames["a"] = directorVideoFrame{Key: "actual-last.png", ClipKey: "a.mp4"}
			if e := directorEncode(&s.row, &d); e != nil {
				t.Fatal(e)
			}
			in.Project = d.Project
			in.ShotID = "b"
			in.Generation = "gb"
			in.ImageKey = "b.png"
			in.VideoModel = "fixture"  // first-frame continuity (no clip extension)
			in.FilmedRecipe = "pose3d" // legacy recipe: continue with the raw last frame
			j.TargetID = "video:b"
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			if len(images.calls) != 0 {
				t.Fatalf("later shot of a %s scene must not be re-cast through Seedream: %+v", style, images.calls)
			}
			if len(v.CreatedRequests) != 1 || len(v.CreatedRequests[0].Content) != 2 {
				t.Fatalf("unexpected video request for %s: %+v", style, v.CreatedRequests)
			}
			request := v.CreatedRequests[0]
			if request.Content[1].Role != modelark.RoleFirstFrame || !strings.Contains(request.Content[1].URL, "actual-last.png") || !strings.Contains(request.Content[0].Text, "actual final frame of the preceding video") {
				t.Fatalf("filmed shot must start from the previous clip's last frame for %s: %+v", style, request)
			}
			var task directorVideoTask
			if e := json.Unmarshal(s.persisted, &task); e != nil {
				t.Fatal(e)
			}
			if task.SourceClipKey != "a.mp4" || task.FirstImageKey != "actual-last.png" {
				t.Fatalf("source lineage lost: %+v", task)
			}
		})
	}
}

// Default filmed recipe: later shots start on the previous clip's real last frame as first_frame,
// even on models that support clip extension (realistic reference videos are rejected).
func TestDirectorContinuityFilmedDefaultUsesRawLastFrameWithoutExtension(t *testing.T) {
	t.Parallel()
	f, s, v, images, _, j, in := syntheticFixture(t) // Seedance 2.0 Fast: supports extension
	d := continuityDoc(t, s.continuityStore)
	d.Project.Style = "cinematic"
	d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
	d.Keys["video:a"] = "a.mp4"
	d.VideoFrames["a"] = directorVideoFrame{Key: "actual-last.png", ClipKey: "a.mp4"}
	if e := directorEncode(&s.row, &d); e != nil {
		t.Fatal(e)
	}
	in.Project = d.Project
	in.ShotID, in.Generation, in.ImageKey = "b", "gb", "b.png"
	j.TargetID = "video:b"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if len(images.calls) != 0 || len(v.CreatedRequests) != 1 || len(v.CreatedRequests[0].Content) != 2 {
		t.Fatalf("unexpected requests: images=%+v videos=%+v", images.calls, v.CreatedRequests)
	}
	r := v.CreatedRequests[0]
	if r.Content[1].Role != modelark.RoleFirstFrame || !strings.Contains(r.Content[1].URL, "actual-last.png") {
		t.Fatalf("continuation must start on the raw last frame: %+v", r)
	}
	var task directorVideoTask
	if e := json.Unmarshal(s.persisted, &task); e != nil {
		t.Fatal(e)
	}
	if !task.RawFrame || task.SourceClipKey != "a.mp4" || task.FirstImageKey != "actual-last.png" || task.Extension {
		t.Fatalf("task lineage: %+v", task)
	}
}

func TestDirectorContinuityFinalizeRejectsNonAdjacentAncestor(t *testing.T) {
	f, s, _, _, _, _ := newContinuityFixture(t)
	f.options.Enabled = true
	f.options.AnalysisModel = "fixture"
	f.options.ImageModel = "fixture"
	f.options.StoryPoints = 1
	f.options.ImagePoints = 1
	f.options.DraftPoints = 8
	d := continuityDoc(t, s)
	d.Project.Storyboard.VideoModelID = "seedance-2-5"
	d.Project.Scenes[0].Shots = append(d.Project.Scenes[0].Shots, dto.DirectorShot{ID: "c", SceneID: "s1", Image: dto.DirectorImage{Status: "succeeded"}, Video: &dto.DirectorVideo{Status: "succeeded", Draft: true}})
	for k := range d.Project.Scenes[0].Shots {
		sh := &d.Project.Scenes[0].Shots[k]
		sh.Video.Status = "succeeded"
		sh.Video.Draft = true
		d.Keys[sh.ID] = sh.ID + ".png"
		d.Keys[directorVideoTarget(sh.ID)] = sh.ID + ".mp4"
		d.VideoDrafts[sh.ID] = directorVideoDraft{TaskID: sh.ID, CreatedAt: time.Now()}
	}
	d.VideoFrames["c"] = directorVideoFrame{SourceClipKey: "b.mp4"}
	ids := []string{"a", "c"}
	queued := 0
	if e := f.enqueueVideos(&d, req.DirectorRequest{ShotIDs: &ids, Finalize: true}, func(string, string, int) error { queued++; return nil }); e == nil || queued != 0 {
		t.Fatalf("selected ancestor charged dependent finalize: err %v queued %d", e, queued)
	}
}
