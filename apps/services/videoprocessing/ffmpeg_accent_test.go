package videoprocessing

import (
	"path/filepath"
	"testing"
)

func TestAccentAndRoundedBoxValidation(t *testing.T) {
	t.Parallel()
	ok := TextLayer{AccentText: "ให้ได้", AccentColor: "FFC400"}
	if !validTextStyle(ok) || validTextStyle(TextLayer{AccentText: "x"}) || validTextStyle(TextLayer{AccentColor: "FFC400"}) || validTextStyle(TextLayer{AccentText: "a\nb", AccentColor: "FFC400"}) {
		t.Fatal("accent needs a one-line phrase and a colour")
	}
	if !rasterTextNeeded(TextLayer{EditorStyle: true, BoxOpacity: .5}) || !rasterTextNeeded(TextLayer{EditorStyle: true, AccentText: "x"}) || rasterTextNeeded(TextLayer{EditorStyle: true, OutlineWidth: 4}) {
		t.Fatal("boxes and accents are rasterized; plain text stays drawtext")
	}
	if r := textBoxRadius(100, 400, 80); r != 40 {
		t.Fatalf("radius is capped at half the box: %v", r)
	}
}

func TestRealAccentColoursOnlyThePhrase(t *testing.T) {
	ffmpegAvailable(t)
	p := NewFFmpegProcessor(filepath.Join(t.TempDir(), "missing.ttf"))
	if !p.supportsEditorText(t.Context()) {
		t.Skip("NOT_RUN: FFmpeg lacks drawtext text_align (needs 6.1+)")
	}
	// Two lines, the second one golden; then one line with a golden word on the right.
	out := styledRender(t, p, TextLayer{Text: "AAAA\nBBBB", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 120, Color: "FFFFFF", AccentText: "BBBB", AccentColor: "FFC400", Font: TextFontHeavy})
	_, _, y0, y1 := litExtent(t, out, "0.5", 1280, 720)
	top, bottom := region(t, out, "0.5", 560, y0+(y1-y0)/4-6, 160, 12), region(t, out, "0.5", 560, y0+3*(y1-y0)/4-6, 160, 12)
	if top[2] < 60 || bottom[2] > top[2]/2 || bottom[0] < 60 {
		t.Fatalf("first line should stay white and the second turn golden: top=%v bottom=%v", top, bottom)
	}
	word := styledRender(t, p, TextLayer{Text: "WWW GGG", StartTime: 0, EndTime: 1, X: .5, Y: .5, FontSize: 120, Color: "FFFFFF", AccentText: "GGG", AccentColor: "FFC400", Font: TextFontHeavy, BoxColor: "000000", BoxOpacity: .5})
	x0, x1, wy0, wy1 := litExtent(t, word, "0.5", 1280, 720)
	mid := wy0 + (wy1-wy0)/2 - 6
	left, right := region(t, word, "0.5", x0+30, mid, 80, 12), region(t, word, "0.5", x1-110, mid, 80, 12)
	if left[2] < 50 || right[2] > right[0]/2 {
		t.Fatalf("only the accent word should be golden: left=%v right=%v", left, right)
	}
}
