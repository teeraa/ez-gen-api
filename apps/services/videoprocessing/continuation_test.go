package videoprocessing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func continuationFixture(t *testing.T, duration string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mp4")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=320x240:r=30:d=" + duration, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=" + duration, "-vf", "drawbox=color=blue:t=fill:enable='gte(t,2)'", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-y", path}
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, out)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return data
}
func TestPrepareContinuationUsesOnlyLastThreeSecondsAndPreservesAudio(t *testing.T) {
	t.Parallel()
	data := continuationFixture(t, "5")
	p := NewFFmpegProcessor("")
	tail, e := p.PrepareContinuation(context.Background(), data)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "tail.mp4")
	if e = os.WriteFile(path, tail, 0600); e != nil {
		t.Fatal(e)
	}
	info, e := probe(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	if info.Duration < 2.99 || info.Duration > 3.05 || info.Width != 960 || info.Height != 720 || !info.Audio {
		t.Fatalf("tail not bounded/compliant: %+v", info)
	}
	pixel, e := exec.Command("ffmpeg", "-v", "error", "-i", path, "-frames:v", "1", "-vf", "scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-").Output()
	if e != nil {
		t.Fatal(e)
	}
	if len(pixel) != 3 || pixel[2] < 200 || pixel[0] > 50 {
		t.Fatalf("tail retained source prefix: rgb %v", pixel)
	}
	if e = p.ValidateContinuationOutput(context.Background(), data, 5); e != nil {
		t.Fatal(e)
	}
	if e = p.ValidateContinuationOutput(context.Background(), tail, 5); e == nil {
		t.Fatal("unexpected output length accepted")
	}
}
func TestContinuationRejectsCombinedAndShortInvalidOrCancelledInputs(t *testing.T) {
	t.Parallel()
	p := NewFFmpegProcessor("")
	combined := continuationFixture(t, "8")
	if e := p.ValidateContinuationOutput(context.Background(), combined, 5); e == nil {
		t.Fatal("input+new cumulative output accepted")
	}
	short := continuationFixture(t, "1")
	if _, e := p.PrepareContinuation(context.Background(), short); e == nil {
		t.Fatal("undersized source accepted")
	}
	if _, e := p.PrepareContinuation(context.Background(), []byte("invalid")); e == nil {
		t.Fatal("invalid input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.PrepareContinuation(ctx, combined); e == nil {
		t.Fatal("cancelled processing accepted")
	}
}
