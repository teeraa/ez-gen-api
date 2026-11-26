package videoprocessing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilmPrintLUTEmbedded(t *testing.T) {
	if !bytes.Contains(filmPrintLUT, []byte("LUT_3D_SIZE 33")) || bytes.Count(filmPrintLUT, []byte("\n")) != 33*33*33+5 {
		t.Fatalf("embedded film look is not a 33-point cube")
	}
}

func TestFinishPartsOrderAndLabels(t *testing.T) {
	d := t.TempDir()
	parts, e := finishParts(FinishFilm, "[cv]", "[fv]", d, 1280, 720)
	if e != nil {
		t.Fatal(e)
	}
	graph := strings.Join(parts, ";")
	for _, want := range []string{"[cv]format=gbrp,lut3d=file='" + filepath.Join(d, "film_print.cube") + "'", "scale=320:180,gblur", "scale=1280:720[fhalo]", "blend=all_mode=screen", "noise=c0s=9:c0f=t+u[fv]"} {
		if !strings.Contains(graph, want) {
			t.Errorf("finish graph lacks %q: %s", want, graph)
		}
	}
	if data, e := os.ReadFile(filepath.Join(d, "film_print.cube")); e != nil || !bytes.Equal(data, filmPrintLUT) {
		t.Fatal("LUT not written beside the graph")
	}
	if _, e := finishParts("vivid", "[cv]", "[fv]", d, 1280, 720); e == nil {
		t.Fatal("unknown finish accepted")
	}
}

func TestMergeRejectsUnknownFinish(t *testing.T) {
	_, e := NewFFmpegProcessor("").Merge(context.Background(), MergeRequest{Clips: []Clip{{Path: "x.mp4"}}, OutputResolution: "720p", OutputAspectRatio: "16:9", AudioVolume: 1, Finish: "vivid"})
	if e == nil || !strings.Contains(e.Error(), "finish") {
		t.Fatalf("want invalid finish error, got %v", e)
	}
}

// A saturated teal frame comes out less saturated and less teal; a text layer drawn after the
// finish keeps its exact colour (titles are not graded).
func TestRealMergeFilmFinish(t *testing.T) {
	ffmpegAvailable(t)
	d := t.TempDir()
	teal := testClip(t, d, "teal", "0x10B0B8", "160x90", "30", true)
	plain := render(t, MergeRequest{Clips: []Clip{{Path: teal}}, OutputResolution: "720p", OutputAspectRatio: "16:9", AudioVolume: 1})
	graded := render(t, MergeRequest{Clips: []Clip{{Path: teal}}, OutputResolution: "720p", OutputAspectRatio: "16:9", AudioVolume: 1, Finish: FinishFilm})
	spread := func(p []byte) int { return max(int(p[0]), int(p[1]), int(p[2])) - min(int(p[0]), int(p[1]), int(p[2])) }
	a, b := pixel(t, plain, "0.5"), pixel(t, graded, "0.5")
	if spread(b) >= spread(a)-10 {
		t.Fatalf("film finish did not desaturate teal: plain %v graded %v", a, b)
	}
	if int(b[0])-int(a[0]) < 5 {
		t.Fatalf("film finish did not pull teal toward neutral (red up): plain %v graded %v", a, b)
	}
}
