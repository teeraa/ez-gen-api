package videoprocessing

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// litCount counts frame pixels brighter than 128 (grey).
func litCount(t *testing.T, path, at string) int {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, v := range out {
		if v > 128 {
			n++
		}
	}
	return n
}

func TestBundledKanitCoversThai(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{"regular": kanitRegular, "heavy": kanitExtraBold} {
		if !fontCovers(data, append(thaiProbe, 'S', '!', '9')) {
			t.Fatalf("%s font lacks Thai or Latin glyphs", name)
		}
	}
	p := NewFFmpegProcessor(filepath.Join(t.TempDir(), "missing.ttf"))
	if !bytes.Equal(p.baseFont([]TextLayer{{Text: "สวัสดี"}}), kanitRegular) {
		t.Fatal("missing font must fall back to bundled Kanit")
	}
	if p := NewFFmpegProcessor(latinFont); FontCoversThai(latinFont) || bytes.Equal(p.baseFont([]TextLayer{{Text: "BOX"}}), kanitRegular) || !bytes.Equal(p.baseFont([]TextLayer{{Text: "ก"}}), kanitRegular) {
		t.Fatal("a Latin-only font is kept for Latin text and replaced for Thai")
	}
}

func TestRealHeavyThaiTextIsBolder(t *testing.T) {
	ffmpegAvailable(t)
	p := NewFFmpegProcessor(filepath.Join(t.TempDir(), "missing.ttf"))
	if !p.supportsEditorText(context.Background()) {
		t.Skip("NOT_RUN: FFmpeg lacks drawtext text_align (needs 6.1+)")
	}
	for _, scale := range []float64{0, 1.4} {
		layer := TextLayer{Text: "ต้องหยุดคนให้ได้", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 120, Color: "FFFFFF"}
		if scale > 0 {
			layer.ScaleX, layer.ScaleY = scale, 1
		}
		heavy := layer
		heavy.Font = TextFontHeavy
		regular, bold := litCount(t, styledRender(t, p, layer), "0.5"), litCount(t, styledRender(t, p, heavy), "0.5")
		if regular < 2000 || float64(bold) < float64(regular)*1.3 {
			t.Fatalf("scale %v: heavy text should cover clearly more pixels: regular %d heavy %d", scale, regular, bold)
		}
	}
	if _, e := p.Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: testClip(t, t.TempDir(), "v", "black", "320x180", "25", false)}}, OutputAspectRatio: "16:9", TextLayers: []TextLayer{{Text: "x", EndTime: 1, Font: "comic"}}}); e == nil {
		t.Fatal("unknown font must be rejected")
	}
}
