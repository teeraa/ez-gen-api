package videoprocessing

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func pngFixture(t *testing.T, dir, color, size string) string {
	t.Helper()
	path := filepath.Join(dir, color+".png")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c="+color+":s="+size, "-frames:v", "1", path).CombinedOutput(); e != nil {
		t.Fatalf("png fixture: %v %s", e, out)
	}
	return path
}

func TestRealMergeImageLayerPlacementTimingAndOpacity(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	video := testClip(t, d, "black", "black", "1280x720", "25", false)
	logo := pngFixture(t, d, "red", "200x100")
	out := render(t, MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", ImageLayers: []ImageLayer{
		{Path: logo, StartTime: .4, EndTime: 5, X: .25, Y: .25, Width: .2, Opacity: 1},
		{Path: logo, StartTime: 0, EndTime: 1, X: .75, Y: .75, Width: .2, Opacity: .5},
	}})
	// Width 20% of 1280 = 256 px wide, 128 px high, centred at (320,180).
	if px := region(t, out, "0.6", 300, 170, 40, 20); px[0] < 200 {
		t.Fatalf("image missing at its centre: %v", px)
	}
	if px := region(t, out, "0.6", 320-128-20, 180, 10, 10); px[0] > 30 {
		t.Fatalf("image wider than 20%% of the frame: %v", px)
	}
	if px := region(t, out, "0.1", 300, 170, 40, 20); px[0] > 30 {
		t.Fatalf("image visible before its start: %v", px)
	}
	half := region(t, out, "0.6", 940, 530, 40, 20)
	if half[0] < 90 || half[0] > 170 {
		t.Fatalf("opacity 0.5 should give about half red: %v", half)
	}
	for _, bad := range []ImageLayer{{Path: logo, StartTime: 0, EndTime: 1, X: .5, Y: .5, Width: .01, Opacity: 1}, {Path: logo, StartTime: 0, EndTime: 1, X: 1.5, Y: .5, Width: .5, Opacity: 1}, {Path: logo, StartTime: 2, EndTime: 3, X: .5, Y: .5, Width: .5, Opacity: 1}, {Path: logo, StartTime: 0, EndTime: 1, X: .5, Y: .5, Width: .5, Opacity: 0}} {
		if _, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", ImageLayers: []ImageLayer{bad}}); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
