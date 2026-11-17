package facade_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

type testLastFrameProcessor struct {
	videoprocessing.VideoProcessor
	calls   int
	inputs  [][]byte
	failure error
}

func (p *testLastFrameProcessor) ExtractLastFrame(_ context.Context, video []byte) ([]byte, error) {
	p.calls++
	p.inputs = append(p.inputs, bytes.Clone(video))
	if p.failure != nil {
		return nil, p.failure
	}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return b.Bytes(), nil
}

type frameRecoveryFetcher struct {
	*continuityMedia
	frameMode string
	urls      []string
}

func (f *frameRecoveryFetcher) Fetch(c context.Context, u string) ([]byte, string, error) {
	f.urls = append(f.urls, u)
	if strings.HasSuffix(u, "frame.png") {
		if f.frameMode == "error" {
			return nil, "", errors.New("expired provider frame")
		}
		if f.frameMode == "invalid" {
			return []byte("invalid"), "image/png", nil
		}
	}
	return f.continuityMedia.Fetch(c, u)
}

func TestDirectorFrameFallbackUsesGeneratedVideoAndNeverResubmits(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "error", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			f, s, v, m, j, in := newContinuityFixture(t)
			extractor := &testLastFrameProcessor{}
			f.options.Processor = extractor
			fetch := &frameRecoveryFetcher{continuityMedia: m, frameMode: mode}
			f.fetcher = fetch
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			result := &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/video.mp4"}
			if mode != "missing" {
				result.LastFrameURL = "https://provider.test/frame.png"
			}
			v.SetTask(v.NextTaskID, result)
			m.failFrame = true
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			var persisted directorVideoTask
			if e := json.Unmarshal(j.Output, &persisted); e != nil {
				t.Fatal(e)
			}
			if persisted.OwnedVideoKey == "" || persisted.PersistenceAttempts != 1 || extractor.calls != 1 {
				t.Fatalf("no persistence checkpoint %+v", persisted)
			}
			// Simulate process restart without access to provider clients: only owned-video persistence may run.
			f.video = nil
			m.failFrame = false
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			d := continuityDoc(t, s)
			if j.Status != "succeeded" || d.VideoFrames[in.ShotID].Key == "" || s.refunds != 0 || len(v.CreatedRequests) != 1 || extractor.calls != 2 {
				t.Fatalf("fallback did not finish without paid retry: %+v", j)
			}
			if !bytes.Equal(extractor.inputs[0], extractor.inputs[1]) {
				t.Fatal("retry did not extract the same generated video")
			}
		})
	}
}
func TestDirectorLegacyFrameRecoveryTickUsesOwnedCheckpointWithoutProvider(t *testing.T) {
	t.Parallel()
	f, s, _, m, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	// Shot b's old provider response was cleared; recovering it must not guess continuity lineage.
	d.Project.Scenes[0].Shots = append(d.Project.Scenes[0].Shots, dto.DirectorShot{ID: "c", SceneID: "s1", Video: &dto.DirectorVideo{Status: "succeeded"}})
	d.Project.Scenes[0].Shots[1].Video.Status = "needs_attention"
	d.Project.Scenes[0].Shots[1].Video.FailureCode = directorPointer("VIDEO_LAST_FRAME_MISSING")
	j.TargetID = "video:b"
	j.Status = "needs_attention"
	in.ShotID = "b"
	in.Generation = "gb"
	in.ImageKey = "b.png"
	j.Snapshot, _ = json.Marshal(in)
	key := "director/" + j.UserID.String() + "/" + j.ProjectID.String() + "/b/" + j.ID.String() + ".mp4"
	j.Output, _ = json.Marshal(directorVideoTask{OwnedVideoKey: key, Recovered: true})
	_ = directorEncode(&s.row, &d)
	f.store = &syntheticTestStore{continuityStore: s, claimed: j}
	f.video = nil
	f.fetcher = m
	f.options.Processor = &testLastFrameProcessor{}
	if e := f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	d = continuityDoc(t, s)
	if j.Status != "succeeded" || d.Keys[j.TargetID] != key || !d.Project.Scenes[0].Shots[1].Video.Stale || !d.Project.Scenes[0].Shots[2].Video.Stale || s.refunds != 0 {
		t.Fatalf("legacy recovery not safely published: %+v", d.Project.Scenes[0].Shots)
	}
}
func TestDirectorFrameRecoveryMissingOwnedVideoStopsAfterThreeAttempts(t *testing.T) {
	t.Parallel()
	f, s, _, _, j, in := newContinuityFixture(t)
	f.video = nil
	f.options.Processor = &testLastFrameProcessor{}
	calls := 0
	f.options.VideoFetch = func(context.Context, string) ([]byte, string, error) {
		calls++
		return nil, "", errors.New("missing owned video")
	}
	key := "director/" + j.UserID.String() + "/" + j.ProjectID.String() + "/a/" + j.ID.String() + ".mp4"
	j.Output, _ = json.Marshal(directorVideoTask{OwnedVideoKey: key, Recovered: true})
	for k := 0; k < 4; k++ {
		if e := f.directorVideo(context.Background(), j, in); e != nil {
			t.Fatal(e)
		}
	}
	d := continuityDoc(t, s)
	failure := d.Project.Scenes[0].Shots[0].Video.FailureCode
	if calls != 3 || j.Status != "needs_attention" || failure == nil || *failure != "VIDEO_FRAME_RECOVERY_FAILED" || s.refunds != 0 {
		t.Fatal("owned-media recovery retried unboundedly or changed billing")
	}
}

func TestDirectorFrameValidProviderFrameSkipsExtraction(t *testing.T) {
	t.Parallel()
	f, s, v, _, j, in := newContinuityFixture(t)
	p := &testLastFrameProcessor{failure: errors.New("must not extract")}
	f.options.Processor = p
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	v.SetTask(v.NextTaskID, &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/video.mp4", LastFrameURL: "https://provider.test/frame.png"})
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if p.calls != 0 || j.Status != "succeeded" || len(continuityDoc(t, s).VideoFrames) != 1 {
		t.Fatal("valid provider frame was needlessly extracted")
	}
}
func TestDirectorFrameRecoveryRejectsTamperedKeyWithoutFetch(t *testing.T) {
	t.Parallel()
	f, s, _, _, j, in := newContinuityFixture(t)
	f.video = nil
	calls := 0
	f.options.VideoFetch = func(context.Context, string) ([]byte, string, error) { calls++; return nil, "", nil }
	j.Output, _ = json.Marshal(directorVideoTask{OwnedVideoKey: "other-owner/video.mp4", Recovered: true})
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	if calls != 0 || j.Status != "needs_attention" || s.refunds != 0 {
		t.Fatal("tampered owned key fetched or refunded")
	}
}
func TestDirectorFrameRecoveryGenerationChangedCancelsWithoutRefund(t *testing.T) {
	t.Parallel()
	f, s, _, _, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	d.Generations[j.TargetID] = "new-generation"
	_ = directorEncode(&s.row, &d)
	j.Status = "needs_attention"
	j.Snapshot, _ = json.Marshal(in)
	j.Output, _ = json.Marshal(directorVideoTask{OwnedVideoKey: "unused", Recovered: true})
	f.store = &syntheticTestStore{continuityStore: s, claimed: j}
	f.video = nil
	if e := f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if j.Status != "cancelled" || s.refunds != 0 {
		t.Fatal("old generated recovery refunded or published")
	}
}
