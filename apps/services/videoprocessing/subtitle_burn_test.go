package videoprocessing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// subtitleBottom counts near-white pixels in the bottom fifth of a frame at time ts.
func subtitleBottom(t *testing.T, path, ts string) int {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", ts, "-i", path, "-frames:v", "1", "-vf", "crop=iw:ih/5:0:ih*4/5,format=gray", "-f", "rawvideo", "-").Output()
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, b := range out {
		if b > 230 {
			n++
		}
	}
	return n
}

func TestMergeBurnsThaiSubtitlesOnTheMergedTimeline(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil || !SupportsSubtitles(context.Background()) {
		t.Skip("NOT_RUN: FFmpeg without libass subtitles")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp4")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=0x404040:s=640x360:r=30:d=2", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", clip).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, out)
	}
	srt := filepath.Join(dir, "s.srt")
	// The cue sits in the second clip: 2.5–3.5 s on the merged timeline.
	if e := os.WriteFile(srt, []byte("1\n00:00:02,500 --> 00:00:03,500\nไม่เป็นไรเลย ค่อย ๆ คิด\nบรรทัดที่สอง\n\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	p := render(t, MergeRequest{Clips: []Clip{{Path: clip}, {Path: clip}}, OutputResolution: "720p", OutputAspectRatio: "16:9", AudioVolume: 1, Subtitles: srt})
	if n := subtitleBottom(t, p, "1.0"); n != 0 {
		t.Fatalf("no subtitle before its cue: %d bright pixels", n)
	}
	if n := subtitleBottom(t, p, "3.0"); n < 200 {
		t.Fatalf("subtitle drawn during its cue: %d bright pixels", n)
	}
	if _, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: clip}}, OutputAspectRatio: "16:9", AudioVolume: 1, Subtitles: filepath.Join(dir, "missing.srt")}); e == nil {
		t.Fatal("a missing subtitle file fails the merge")
	}
}
