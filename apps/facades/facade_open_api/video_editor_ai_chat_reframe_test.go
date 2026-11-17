package facade_open_api

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
)

func TestAIReframeLookKeepsTheSubjectInFrame(t *testing.T) {
	// A tall subject on the right of a 16:9 clip shown in 9:16: cover already crops, so only pan.
	zoom, panX, panY := aiReframeLook(640, 360, 9, 16, aiReframeBox{X0: .6, Y0: .2, X1: .8, Y1: .95}, "medium")
	if zoom != 1 || panX < .5 || panY != 0 {
		t.Fatalf("got zoom %v pan %v,%v", zoom, panX, panY)
	}
	// A small centred face with tight framing zooms in (capped at 2.5) and pans up to the face.
	zoom, panX, panY = aiReframeLook(640, 360, 9, 16, aiReframeBox{X0: .45, Y0: .3, X1: .55, Y1: .5}, "tight")
	if zoom != 2.5 || panX != 0 || panY >= 0 {
		t.Fatalf("got zoom %v pan %v,%v", zoom, panX, panY)
	}
	visible := 9 / (640 * (16.0 / 360) * zoom)
	if left := (.5 + panX/2) * (1 - visible); left > .45 || left+visible < .55 {
		t.Fatalf("face outside the visible window %v..%v", left, left+visible)
	}
	// Same aspect and wide framing: nothing overflows, so nothing moves.
	if zoom, panX, _ = aiReframeLook(640, 360, 16, 9, aiReframeBox{X1: .3, Y1: .3}, "wide"); zoom != 1 || panX != 0 {
		t.Fatalf("got zoom %v pan %v", zoom, panX)
	}
	if w, h := aiAspect("1:1"); w != h {
		t.Fatal("1:1 aspect")
	}
}

func TestAIEffectsAcceptMotionTools(t *testing.T) {
	for _, name := range []string{"zoomin", "hblur", "smoothleft", "radial"} {
		if !veTransitions[name] || !strings.Contains(aiSegmentEffectSchema, `"`+name+`"`) || !strings.Contains(aiEditRules, name) {
			t.Fatalf("transition %s missing from validator, plan schema or chat rules", name)
		}
	}
	if aiEditTransitionName("Zoom") != "zoomin" || aiEditTransitionName("blur") != "hblur" || aiEditTransitionName("dissolve") != "fade" {
		t.Fatal("transition aliases")
	}
	look := aiValidLook(aiValidLook(modelsLook(1.2, 1.5)))
	if look.Zoom != 1.2 || look.ZoomTo != 1.5 {
		t.Fatalf("zoomTo dropped: %+v", look)
	}
	if look = aiValidLook(modelsLook(0, 5)); look.ZoomTo != 0 {
		t.Fatalf("zoomTo above 3 must be dropped: %+v", look)
	}
	if !strings.Contains(aiTextStyleSchema, `"pop"`) {
		t.Fatal("pop animation missing from the plan schema")
	}
}

func modelsLook(zoom, zoomTo float64) models.VideoEditorClipLook {
	return models.VideoEditorClipLook{Zoom: zoom, ZoomTo: zoomTo}
}

func TestVETextAccentAndHeavyFont(t *testing.T) {
	l := aiReferenceTextLayer()
	l.Text, l.Font, l.AccentText, l.AccentColor = "5 วินาทีแรก\nต้องหยุดคนให้ได้", "heavy", " ต้องหยุดคนให้ได้ ", "#FFC400"
	out, err := normalizeVETextLayers([]models.VideoEditorTextLayer{l})
	if err != nil || out[0].AccentText != "ต้องหยุดคนให้ได้" || out[0].Font != "heavy" {
		t.Fatalf("valid accent rejected: %v %+v", err, out)
	}
	if r := veRenderText(out); r[0].AccentColor != "FFC400" || r[0].Font != "heavy" {
		t.Fatalf("render mapping lost the style: %+v", r[0])
	}
	l.AccentText = "ไม่มีในข้อความ"
	if out, err = normalizeVETextLayers([]models.VideoEditorTextLayer{l}); err != nil || out[0].AccentText != "" || out[0].AccentColor != "" {
		t.Fatalf("a missing accent phrase must be dropped, not fail the save: %v %+v", err, out)
	}
	for _, bad := range []models.VideoEditorTextLayer{{AccentText: "แรก", AccentColor: "gold"}, {AccentColor: "#FFC400"}, {Font: "comic"}} {
		c := aiReferenceTextLayer()
		c.Text, c.Font, c.AccentText, c.AccentColor = "5 วินาทีแรก", bad.Font, bad.AccentText, bad.AccentColor
		if _, err := normalizeVETextLayers([]models.VideoEditorTextLayer{c}); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
