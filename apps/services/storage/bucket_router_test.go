package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBucketRouterSupabasePaths(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		if req.Header.Get("Authorization") != "Bearer test-service-role" {
			t.Error("missing server credential")
		}
		if strings.Contains(req.URL.Path, "/sign/") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"signedURL":"/object/sign/test?token=fixture"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	legacy, _ := NewSupabaseClient(server.URL, "test-service-role", "legacy-video", server.Client())
	images, _ := NewSupabaseClient(server.URL, "test-service-role", "private-characters", server.Client())
	router := NewBucketRouter(legacy, images)
	for _, key := range []string{KeyForCharacter("user", "job", "png"), "outputs/user/old-job/character.png", "outputs/user/video.mp4", "uploads/user/reference.png", "thumbs/user/video.jpg", "exports/user/video.mp4"} {
		bucket := "legacy-video"
		if strings.HasPrefix(key, "characters/") {
			bucket = "private-characters"
		}
		if err := router.Upload(context.Background(), key, []byte("fixture"), "image/png"); err != nil {
			t.Fatal(err)
		}
		if paths[len(paths)-1] != "/storage/v1/object/"+bucket+"/"+key {
			t.Fatal("upload wrong bucket", paths)
		}
		if _, _, err := router.SignedURL(context.Background(), key, time.Minute); err != nil {
			t.Fatal(err)
		}
		if paths[len(paths)-1] != "/storage/v1/object/sign/"+bucket+"/"+key {
			t.Fatal("preview wrong bucket", paths)
		}
		signed, _, err := router.SignedDownloadURL(context.Background(), key, time.Minute, "character.png")
		if err != nil || !strings.Contains(signed, "download=character.png") {
			t.Fatal("download failed", err)
		}
		if paths[len(paths)-1] != "/storage/v1/object/sign/"+bucket+"/"+key {
			t.Fatal("download wrong bucket", paths)
		}
	}
}
func TestBucketRouterDoesNotFallbackAfterImageUploadFailure(t *testing.T) {
	legacy, images := NewMemoryClient(), NewMemoryClient()
	images.UploadErr = errors.New("bucket unavailable")
	router := NewBucketRouter(legacy, images)
	key := KeyForCharacter("user", "job", "png")
	if router.Upload(context.Background(), key, []byte("fixture"), "image/png") == nil {
		t.Fatal("image failure swallowed")
	}
	if _, ok := legacy.Object(key); ok {
		t.Fatal("image leaked into legacy bucket")
	}
	if err := router.Upload(context.Background(), "outputs/user/video.mp4", []byte("video"), "video/mp4"); err != nil {
		t.Fatal("image failure affected video", err)
	}
}
