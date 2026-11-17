package seedaudio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateSendsModelAndKeyAndDecodesAudio(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/tts/create" || r.Header.Get("X-Api-Key") != "k" || r.Header.Get("X-Api-Request-Id") == "" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != Model || body["text_prompt"] != "สวัสดี" {
			t.Errorf("bad body %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "audio": base64.StdEncoding.EncodeToString([]byte("ID3abc")), "duration": 1.5,
			"subtitle": map[string]any{"sentences": []any{map[string]any{"start_time": 0, "end_time": 1500, "text": "สวัสดี"}}}})
	}))
	defer srv.Close()
	out, e := New("k", srv.URL).Generate(context.Background(), Request{TextPrompt: "สวัสดี", Audio: AudioConfig{EnableSubtitle: true}})
	if e != nil || string(out.Audio) != "ID3abc" || out.Duration != 1.5 || len(out.Sentences) != 1 || out.Format != "mp3" {
		t.Fatalf("got %+v, %v", out, e)
	}
}

func TestGenerateDownloadsURLAndMapsRejections(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/file.mp3", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ID3xyz")) })
	mux.HandleFunc("/api/v3/tts/create", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["text_prompt"] == "bad" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 40000, "message": "invalid"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "url": srv.URL + "/file.mp3"})
	})
	c := New("k", srv.URL)
	out, e := c.Generate(context.Background(), Request{TextPrompt: "ok"})
	if e != nil || string(out.Audio) != "ID3xyz" {
		t.Fatalf("download: %+v %v", out, e)
	}
	if _, e = c.Generate(context.Background(), Request{TextPrompt: "bad"}); !errors.Is(e, ErrRejected) {
		t.Fatalf("400 must be a rejection: %v", e)
	}
	if New("", "") != nil {
		t.Fatal("no key means no client")
	}
}
