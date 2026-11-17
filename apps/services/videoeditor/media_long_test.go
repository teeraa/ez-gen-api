package videoeditor

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLongAudioAllowedUntilAnHour(t *testing.T) {
	d := t.TempDir()
	mk := func(name string, seconds string, args ...string) string {
		p := filepath.Join(d, name)
		base := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "anullsrc=r=8000:cl=mono", "-t", seconds}
		if out, e := exec.Command("ffmpeg", append(append(base, args...), p)...).CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, out)
		}
		return p
	}
	long := mk("long.mp3", "1500", "-c:a", "libmp3lame", "-b:a", "8k")
	m, e := Inspect(context.Background(), long)
	if e != nil || m.Kind != "audio" || m.Duration < 1499 {
		t.Fatalf("a 25 min mp3 should be accepted: %+v %v", m, e)
	}
	tooLong := mk("too-long.mp3", "3700", "-c:a", "libmp3lame", "-b:a", "8k")
	if _, e := Inspect(context.Background(), tooLong); !errors.Is(e, ErrTooLong) || !errors.Is(e, ErrInvalid) {
		t.Fatalf("over an hour must be ErrTooLong (still ErrInvalid): %v", e)
	}
}
