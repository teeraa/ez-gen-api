package videoprocessing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMixAudioPlacesTracksOnTimeline(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.mp3")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-y", tone).CombinedOutput(); e != nil {
		t.Fatalf("make tone: %v %s", e, out)
	}
	data, e := MixAudio(context.Background(), []AudioTrack{{Path: tone, TrimEnd: 1, At: 0.4, Volume: 1}, {Path: tone, TrimEnd: 1, At: 2, Volume: 1}}, 4)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "mix.wav")
	if e = os.WriteFile(path, data, 0o600); e != nil {
		t.Fatal(e)
	}
	d, e := AudioDuration(context.Background(), path)
	if e != nil || d < 3.95 || d > 4.05 {
		t.Fatalf("the mix lasts the timeline: %v %v", d, e)
	}
	if _, e := MixAudio(context.Background(), nil, 4); e == nil {
		t.Fatal("an empty mix is rejected")
	}
}
