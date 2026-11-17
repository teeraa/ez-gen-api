package facade_open_api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestDirectorExactFirstFrameUsesPreviousClipWithoutReferenceImages(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"seedance-1-0-pro-250528"} {
		t.Run(provider, func(t *testing.T) {
			f, s, v, _, j, in := newContinuityFixture(t)
			d := continuityDoc(t, s)
			d.Project.Style = "anime"
			d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
			d.Keys["video:a"] = "current-previous.mp4"
			d.VideoFrames["a"] = directorVideoFrame{Key: "actual-last.png", ClipKey: "current-previous.mp4"}
			sh := &d.Project.Scenes[0].Shots[1]
			sh.CharacterAssetIDs = []string{"actor"}
			sh.Description = "Actor walks onward"
			_ = directorEncode(&s.row, &d)
			j.TargetID = "video:b"
			in.ShotID = "b"
			in.Generation = "gb"
			in.ImageKey = "b.png"
			in.VideoModel = provider
			in.Characters = []directorCharacter{{ID: "actor", Name: "Actor", Description: "green coat", Key: "raw-actor.png"}}
			if e := f.directorVideo(context.Background(), j, in); e != nil {
				t.Fatal(e)
			}
			r := v.CreatedRequests[0]
			if len(r.Content) != 2 || r.Content[1].Role != modelark.RoleFirstFrame || r.Content[1].URL != "https://owned.test/actual-last.png" {
				t.Fatalf("not strict previous-frame input: %+v", r)
			}
			if !strings.Contains(r.Content[0].Text, "green coat") || strings.Contains(r.Content[0].Text, "Image 2") || strings.Contains(r.Content[0].Text, "raw-actor") {
				t.Fatalf("cast text needs unavailable image: %s", r.Content[0].Text)
			}
			var task directorVideoTask
			if e := json.Unmarshal(j.Output, &task); e != nil {
				t.Fatal(e)
			}
			if task.SourceClipKey != "current-previous.mp4" || task.FirstImageKey != "actual-last.png" {
				t.Fatalf("source identity lost: %+v", task)
			}
		})
	}
}

func TestDirectorFirstShotUsesStoryboardAsStrictFirstFrame(t *testing.T) {
	t.Parallel()
	f, _, v, _, j, in := newContinuityFixture(t)
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	r := v.CreatedRequests[0]
	if len(r.Content) != 2 || r.Content[1].Role != modelark.RoleFirstFrame || r.Content[1].URL != "https://owned.test/a.png" {
		t.Fatalf("first shot input: %+v", r)
	}
}

type directorWireRequest struct {
	Model   string `json:"model"`
	Content []struct {
		Type     string `json:"type"`
		Role     string `json:"role"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	} `json:"content"`
}

func TestDirectorPreviousFrameRoleReachesSDKHTTPJSON(t *testing.T) {
	t.Parallel()
	requests := make(chan directorWireRequest, 1)
	decodeErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload directorWireRequest
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			decodeErrors <- e
			w.WriteHeader(400)
			return
		}
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture-exact-frame-task"}`))
	}))
	defer server.Close()
	f, s, _, _, j, in := newContinuityFixture(t)
	d := continuityDoc(t, s)
	d.Project.Style = "anime"
	d.Project.Scenes[0].Shots[0].Video.Status = "succeeded"
	d.Keys["video:a"] = "previous.mp4"
	d.VideoFrames["a"] = directorVideoFrame{Key: "actual-final.png", ClipKey: "previous.mp4"}
	_ = directorEncode(&s.row, &d)
	f.video = modelark.NewClient("fixture-only", server.URL+"/api/v3")
	j.TargetID = "video:b"
	in.ShotID = "b"
	in.Generation = "gb"
	in.ImageKey = "b.png"
	in.VideoModel = "seedance-1-0-pro-250528"
	if e := f.directorVideo(context.Background(), j, in); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-decodeErrors:
		t.Fatal(e)
	default:
	}
	select {
	case got := <-requests:
		if got.Model != in.VideoModel || len(got.Content) != 2 || got.Content[1].Type != "image_url" || got.Content[1].Role != "first_frame" || got.Content[1].ImageURL.URL != "https://owned.test/actual-final.png" {
			t.Fatalf("wrong provider HTTP input: %+v", got)
		}
	default:
		t.Fatal("provider HTTP fixture was never called")
	}
}
