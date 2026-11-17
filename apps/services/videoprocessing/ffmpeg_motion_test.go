package videoprocessing

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// litExtent decodes one grey frame and returns the lit (> threshold) bounding box.
func litExtent(t *testing.T, path, at string, w, h int) (x0, x1, y0, y1 int) {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if e != nil || len(out) < w*h {
		t.Fatalf("frame decode: %v (%d bytes)", e, len(out))
	}
	x0, y0, x1, y1 = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out[y*w+x] > 12 {
				x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
			}
		}
	}
	if x1 < 0 {
		t.Fatalf("nothing lit at %s", at)
	}
	return
}

func TestPopAndZoomFilterStrings(t *testing.T) {
	t.Parallel()
	if got := textPopScale(1, .3); got != "(0.60+0.40*min(1,max(0,(t-1.000000)/0.300000))+0.18*sin(PI*min(1,max(0,(t-1.000000)/0.300000))))" {
		t.Fatal(got)
	}
	if !validTextStyle(TextLayer{Animation: "pop"}) || validTextStyle(TextLayer{Animation: "spin"}) {
		t.Fatal("pop must be the only new animation")
	}
	if got := buildEditorDrawtext(TextLayer{Text: "x", X: .5, Y: .5, FontSize: 100, Animation: "pop"}, 0, 2, 1080, "f", "t"); !strings.Contains(got, ":fontsize='100*(0.60+") {
		t.Fatal(got)
	}
	if !validLook(ClipLook{ZoomTo: 1.5}) || validLook(ClipLook{ZoomTo: .5}) || validLook(ClipLook{ZoomTo: 4}) || validLook(ClipLook{ZoomTo: math.NaN()}) {
		t.Fatal("zoomTo must be 0 or 1..3")
	}
	parts := zoomingParts("[0:v:0]", "trim=start=0:duration=1", ClipLook{Zoom: 1, ZoomTo: 2, Fit: "cover", PanX: 1}, 720, 1280, 2, ",setsar=1", "[v0]", 0)
	if len(parts) != 3 || !strings.Contains(parts[0], "eval=frame") || !strings.Contains(parts[0], "max(720/iw,1280/ih)") || !strings.Contains(parts[2], "-(w-W)*1.000000") || !strings.HasSuffix(parts[2], ",setsar=1[v0]") {
		t.Fatalf("%q", parts)
	}
	for _, name := range []string{"zoomin", "hblur", "smoothleft", "smoothright", "slideup", "slidedown", "pixelize", "fadegrays", "radial"} {
		if !xfadeTransitions[name] {
			t.Fatal(name)
		}
	}
}

func TestRealPopTextGrowsAroundItsCentre(t *testing.T) {
	p := styleProcessor(t)
	for _, scale := range []float64{0, 1.5} { // drawtext path, then the rasterized (scaled) path
		layer := TextLayer{Text: "POP", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 160, Color: "FFFFFF", Animation: "pop"}
		if scale > 0 {
			layer.ScaleX, layer.ScaleY = scale, 1
		}
		out := styledRender(t, p, layer)
		// At u = 0.4 (0.12 s) the curve is 0.6+0.16+0.18·sin(0.4π) ≈ 0.931; at 0.6 s it is 1.
		ex0, ex1, ey0, ey1 := litExtent(t, out, "0.12", 1280, 720)
		fx0, fx1, fy0, fy1 := litExtent(t, out, "0.6", 1280, 720)
		ratio := float64(ex1-ex0) / float64(fx1-fx0)
		if ratio < .85 || ratio > .97 {
			t.Fatalf("scale %v: early/final width ratio %.3f (%d-%d vs %d-%d)", scale, ratio, ex0, ex1, fx0, fx1)
		}
		if math.Abs(float64(ex0+ex1-fx0-fx1)/2) > 6 || math.Abs(float64(ey0+ey1-fy0-fy1)/2) > 8 {
			t.Fatalf("scale %v: pop must grow around the centre: early %d,%d final %d,%d", scale, (ex0+ex1)/2, (ey0+ey1)/2, (fx0+fx1)/2, (fy0+fy1)/2)
		}
	}
}

func TestRealZoomToPushesIn(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	// A small white square in the middle of a black 16:9 clip.
	src := filepath.Join(d, "square.mp4")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=320x180:d=2:r=25", "-vf", "drawbox=x=140:y=70:w=40:h=40:color=white:t=fill", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-t", "2", src).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, out)
	}
	out := render(t, MergeRequest{Clips: []Clip{{Path: src, Look: ClipLook{ZoomTo: 3}}, {Path: src, TrimEnd: .5, Transition: "zoomin", TransitionDuration: .2}}, OutputAspectRatio: "16:9"})
	info, e := probe(context.Background(), out)
	if e != nil || math.Abs(info.Duration-2.3) > .1 {
		t.Fatalf("zoomTo output %+v %v", info, e)
	}
	start, end := pixel(t, out, "0.02"), pixel(t, out, "1.6")
	// Square area is 2.8% of the frame at 1×; ×9 at 3× (eased, ~2.9× by 1.6 s).
	if start[0] > 20 || end[0] < 40 {
		t.Fatalf("zoom should grow the square: start=%v end=%v", start, end)
	}
	if px := pixel(t, out, "1.0"); px[0] <= start[0] || px[0] >= end[0] {
		t.Fatalf("zoom should be gradual: mid=%v", px)
	}
}

func TestRealNewTransitionsRender(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	red, blue := testClip(t, d, "red", "red", "160x90", "25", true), testClip(t, d, "blue", "blue", "160x90", "25", true)
	for _, name := range []string{"zoomin", "hblur", "smoothleft", "smoothright", "slideup", "slidedown", "pixelize", "fadegrays", "radial"} {
		p := render(t, MergeRequest{Clips: []Clip{{Path: red}, {Path: blue, Transition: name, TransitionDuration: .5}}, OutputAspectRatio: "16:9"})
		info, e := probe(context.Background(), p)
		if e != nil || math.Abs(info.Duration-1.5) > .1 {
			t.Fatalf("%s: %+v %v", name, info, e)
		}
		if before, after := pixel(t, p, "0.2"), pixel(t, p, "1.3"); before[0] < 150 || after[2] < 150 {
			t.Fatalf("%s: before=%v after=%v", name, before, after)
		}
	}
}
