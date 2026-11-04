package videoeditor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func stillFixture(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	full := append([]string{"-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=320x200", "-frames:v", "1"}, args...)
	if out, e := exec.Command("ffmpeg", append(full, path)...).CombinedOutput(); e != nil {
		t.Fatalf("fixture %s: %v %s", name, e, out)
	}
	return path
}

func TestInspectStillImages(t *testing.T) {
	if !Available() {
		t.Skip("NOT_RUN: ffmpeg/ffprobe unavailable")
	}
	for name, mime := range map[string]string{"logo.png": "image/png", "photo.jpg": "image/jpeg", "sticker.webp": "image/webp"} {
		path := stillFixture(t, name)
		data, _ := os.ReadFile(path)
		if !Magic(data) {
			t.Fatalf("%s rejected by magic", name)
		}
		m, e := Inspect(context.Background(), path)
		if e != nil {
			t.Fatalf("%s: %v", name, e)
		}
		if m.Kind != "image" || m.MIME != mime || m.Width != 320 || m.Height != 200 || m.Duration != 0 || len(m.Frames) != 1 || len(m.Frames[0].Data) == 0 {
			t.Fatalf("%s inspected as %+v", name, m)
		}
	}
	// A PNG cut in half passes magic but must fail the decode.
	path := stillFixture(t, "cut.png")
	data, _ := os.ReadFile(path)
	if e := os.WriteFile(path, data[:len(data)/2], 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Inspect(context.Background(), path); !errors.Is(e, ErrInvalid) {
		t.Fatalf("truncated png accepted: %v", e)
	}
}
