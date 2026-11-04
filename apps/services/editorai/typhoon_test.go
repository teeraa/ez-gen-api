package editorai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func typhoonFixture(t *testing.T, script string) *TyphoonASR {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"encoder-fastconformer-quran-ar.onnx", "decoder_joint-fastconformer-quran-ar.onnx", "vocab.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	runner := filepath.Join(dir, "runner.sh")
	if err := os.WriteFile(runner, []byte("if [ \"$1\" = --check ]; then exit 0; fi\n"+script), 0600); err != nil {
		t.Fatal(err)
	}
	return &TyphoonASR{Python: "/bin/sh", Runner: runner, Model: dir, Version: "fixture-v1"}
}

func TestTyphoonSelectionAndCacheIsolation(t *testing.T) {
	w := &LocalASR{}
	a := &TyphoonASR{Version: "one", Model: "/model"}
	if SelectTranscriber("", w, a) != w || SelectTranscriber("whisper", w, a) != w || SelectTranscriber("typhoon", w, a) != a {
		t.Fatal("provider selection changed")
	}
	if SelectTranscriber("typo", w, a).Available() {
		t.Fatal("unknown provider silently enabled")
	}
	if a.Signature() == w.Signature() || !strings.Contains(a.Signature(), "one") {
		t.Fatal("cache namespace not independent")
	}
	if a.CacheSignature() == a.Signature() || !strings.HasSuffix(a.CacheSignature(), ":pause-segments-v2") {
		t.Fatal("pause generation must isolate caches without changing stored job identity")
	}
	b := &TyphoonASR{Version: "two", Model: "/model"}
	if a.Signature() == b.Signature() {
		t.Fatal("model version omitted from cache namespace")
	}
}

func TestTyphoonLocalArtifactsAndStrictOutput(t *testing.T) {
	for _, test := range []struct {
		name, output string
		wantErr      error
	}{
		{"valid", `{"transcripts":[{"assetId":"fixture","language":"th","text":"","hasSpeech":false,"segments":[]}]}`, nil},
		{"unknown-field", `{"transcripts":[],"unexpected":true}`, ErrInvalid},
		{"trailing-json", `{"transcripts":[]} {}`, ErrInvalid},
		{"invalid-json", `not-json`, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := typhoonFixture(t, "cat >/dev/null\nprintf '%s' '"+test.output+"'\n")
			rows, err := a.Transcribe(context.Background(), []Audio{{AssetID: "fixture", Path: "/fixture.wav", Duration: 1}})
			if !errors.Is(err, test.wantErr) || err == nil && len(rows) != 1 {
				t.Fatalf("unexpected adapter result: rows=%d err=%v", len(rows), err)
			}
		})
	}
	a := typhoonFixture(t, "exit 0\n")
	if err := os.Remove(filepath.Join(a.Model, "vocab.json")); err != nil {
		t.Fatal(err)
	}
	if a.Available() {
		t.Fatal("missing local vocabulary enabled provider")
	}
}

func TestTyphoonCancellationAndBatchBounds(t *testing.T) {
	a := typhoonFixture(t, "cat >/dev/null\nsleep 10\n")
	if !a.Available() {
		t.Fatal("fixture unavailable")
	}
	if _, err := a.Transcribe(context.Background(), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatal("empty batch accepted")
	}
	if _, err := a.Transcribe(context.Background(), make([]Audio, 9)); !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversized batch accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := a.Transcribe(ctx, []Audio{{AssetID: "fixture", Path: "/fixture.wav", Duration: 1}}); !errors.Is(err, ErrASR) {
		t.Fatal("cancelled subprocess succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancelled subprocess kept running")
	}
}
