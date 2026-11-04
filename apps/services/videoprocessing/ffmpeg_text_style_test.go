package videoprocessing

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const latinFont = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

func styleProcessor(t *testing.T) *FFmpegProcessor {
	t.Helper()
	ffmpegAvailable(t)
	if _, e := os.Stat(latinFont); e != nil {
		t.Skip("NOT_RUN: DejaVu font missing")
	}
	p := NewFFmpegProcessor(latinFont)
	if !p.supportsEditorText(context.Background()) {
		t.Skip("NOT_RUN: FFmpeg lacks drawtext text_align (needs 6.1+)")
	}
	return p
}

// region averages a w×h area at (x,y) into one RGB pixel.
func region(t *testing.T, path, at string, x, y, w, h int) []byte {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-vf", fmt.Sprintf("crop=%d:%d:%d:%d,scale=1:1", w, h, x, y), "-f", "rawvideo", "-pix_fmt", "rgb24", "-").Output()
	if e != nil || len(out) < 3 {
		t.Fatalf("region decode: %v", e)
	}
	return out
}

func styledRender(t *testing.T, p *FFmpegProcessor, layers ...TextLayer) string {
	t.Helper()
	d := t.TempDir()
	video := testClip(t, d, "v", "black", "1280x720", "25", false)
	for i := range layers {
		layers[i].Absolute, layers[i].EditorStyle, layers[i].HasXY = true, true, true
	}
	data, e := p.Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: video, TrimEnd: 1}}, OutputAspectRatio: "16:9", TextLayers: layers})
	if e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(d, "out.mp4")
	if e = os.WriteFile(out, data, 0600); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestEditorTextStyleFilterStrings(t *testing.T) {
	t.Parallel()
	base := TextLayer{EditorStyle: true, HasXY: true, X: .5, Y: .5, FontSize: 100, Color: "FFFFFF"}
	plain := buildEditorDrawtext(base, 1, 3, 1080, "f.ttf", "t.txt")
	if strings.Contains(plain, "borderw") || strings.Contains(plain, "box=1") || strings.Contains(plain, "alpha=") {
		t.Fatalf("unstyled layer gained style: %s", plain)
	}
	styled := base
	styled.OutlineWidth, styled.OutlineColor, styled.BoxColor, styled.BoxOpacity, styled.Animation = 4, "000000", "FF0000", .5, "slide"
	got := buildEditorDrawtext(styled, 1, 3, 720, "f.ttf", "t.txt")
	for _, want := range []string{":borderw=3:bordercolor=0x000000", ":box=1:boxcolor=0xFF0000@0.500000:boxborderw=23", ":alpha='min(1,max(0,min((t-1.000000)/0.300000,(3.000000-t)/0.300000)))'", "+h*0.040000*max(0,1-(t-1.000000)/0.300000)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled drawtext missing %q:\n%s", want, got)
		}
	}
	if math.Abs(textAnimDuration(0, .6)-.2) > 1e-9 {
		t.Fatal("short layers animate over a third of their length")
	}
	for _, bad := range []TextLayer{{OutlineWidth: 21, OutlineColor: "000000"}, {OutlineWidth: 2}, {BoxOpacity: .5}, {BoxOpacity: 2, BoxColor: "FFFFFF"}, {Animation: "spin"}} {
		if validTextStyle(bad) {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestRealEditorTextBoxOutlineAndFade(t *testing.T) {
	p := styleProcessor(t)
	// A full-opacity red box behind white text, fading in over the first 0.3 s.
	out := styledRender(t, p, TextLayer{Text: "BOX", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 120, Color: "FFFFFF", BoxColor: "FF0000", BoxOpacity: 1, Animation: "fade"})
	// Sample just outside the glyphs but inside the box padding (left of centre band).
	if px := region(t, out, "0.6", 526, 294, 12, 12); px[0] < 150 || px[1] > 80 {
		t.Fatalf("box colour missing beside the text: %v", px)
	}
	if px := region(t, out, "0", 526, 294, 12, 12); px[0] > 60 {
		t.Fatalf("fade-in should start transparent: %v", px)
	}
	// An outline-only layer stays transparent between glyph strokes and frame edges.
	outline := styledRender(t, p, TextLayer{Text: "O", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 200, Color: "000000", OutlineColor: "00FF00", OutlineWidth: 12})
	if px := region(t, outline, "0.5", 560, 300, 160, 120); px[1] < 25 {
		t.Fatalf("green outline missing around black glyph: %v", px)
	}
}

func TestRealScaledEditorTextKeepsBoxPadding(t *testing.T) {
	p := styleProcessor(t)
	scale := 1.5
	out := styledRender(t, p, TextLayer{Text: "WIDE", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 80, Color: "FFFFFF", ScaleX: scale, ScaleY: 1, BoxColor: "0000FF", BoxOpacity: 1, Animation: "fade"})
	// The raster includes the box padding: blue must reach beyond the text on both sides.
	left, right := region(t, out, "0.6", 500, 330, 10, 10), region(t, out, "0.6", 768, 330, 10, 10)
	if left[2] < 150 || right[2] < 150 {
		t.Fatalf("scaled box padding clipped: left=%v right=%v", left, right)
	}
	if px := region(t, out, "0", 500, 330, 10, 10); px[2] > 60 {
		t.Fatalf("scaled text should fade in from transparent: %v", px)
	}
}
