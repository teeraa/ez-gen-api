package facade_open_api

import (
	"math"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

func aiDraftTimeline(segments []models.EditorAIDraftSegment) []models.VideoEditorSegment {
	out := make([]models.VideoEditorSegment, 0, len(segments))
	for _, s := range segments {
		out = append(out, s.TimelineSegment())
	}
	return out
}

// aiDraftDuration is the timeline length after speed and transition overlap, the same
// total the editor validates and renders.
func aiDraftDuration(segments []models.EditorAIDraftSegment) float64 {
	return models.VideoEditorTimelineLength(aiDraftTimeline(segments))
}

// aiValidLook keeps each look field the editor validator accepts and drops the rest.
func aiValidLook(l models.VideoEditorClipLook) models.VideoEditorClipLook {
	out := models.VideoEditorClipLook{}
	for _, set := range []func(*models.VideoEditorClipLook){
		func(c *models.VideoEditorClipLook) { c.Brightness = l.Brightness },
		func(c *models.VideoEditorClipLook) { c.Contrast = l.Contrast },
		func(c *models.VideoEditorClipLook) { c.Saturation = l.Saturation },
		func(c *models.VideoEditorClipLook) { c.Fit = l.Fit },
		func(c *models.VideoEditorClipLook) { c.Zoom = l.Zoom },
		func(c *models.VideoEditorClipLook) { c.PanX = l.PanX },
		func(c *models.VideoEditorClipLook) { c.PanY = l.PanY },
		func(c *models.VideoEditorClipLook) { c.ZoomTo = l.ZoomTo },
	} {
		candidate := out
		set(&candidate)
		if veValidLook(candidate) {
			out = candidate
		}
	}
	return out
}

// sanitizeAIDraftEffects resets every effect the manual editor validators would reject
// to unchanged, in timeline order because a transition depends on the previous length.
func sanitizeAIDraftEffects(segments []models.EditorAIDraftSegment) {
	for i := range segments {
		s := &segments[i]
		if !veValidSpeed(s.Speed) || s.Speed == 1 {
			s.Speed = 0
		}
		s.VideoEditorClipLook = aiValidLook(s.VideoEditorClipLook)
		if s.Zoom == 1 {
			s.Zoom = 0
		}
		if s.ZoomTo == s.Zoom || s.ZoomTo == 1 && s.Zoom == 0 {
			s.ZoomTo = 0
		}
		length := s.TimelineSegment().TimelineLength()
		if !veValidFades(s.FadeIn, 0, length) {
			s.FadeIn = 0
		}
		if !veValidFades(s.FadeIn, s.FadeOut, length) {
			s.FadeOut = 0
		}
		if !veValidTransition(aiDraftTimeline(segments[:i+1]), i) {
			s.VideoEditorTransition = models.VideoEditorTransition{}
		}
	}
}

// aiFitTargetDuration drops slow-motion when it would push a generated draft past the
// requested target. Selected windows already fit at 1×, and faster speeds and
// transitions only shorten the timeline, so this always restores the target rule.
func aiFitTargetDuration(segments []models.EditorAIDraftSegment, target float64) {
	if aiDraftDuration(segments) <= target+.05 {
		return
	}
	for i := range segments {
		if segments[i].Speed != 0 && segments[i].Speed < 1 {
			segments[i].Speed = 0
		}
	}
	sanitizeAIDraftEffects(segments)
}

// aiMergeableEffects: merging must not move a fade or transition into the middle of a span.
func aiMergeableEffects(previous, next models.EditorAIDraftSegment) bool {
	return previous.Speed == next.Speed && previous.VideoEditorClipLook == next.VideoEditorClipLook && previous.FadeOut == 0 && next.FadeIn == 0 && next.Transition == "" && next.TransitionDuration == 0
}

func aiReferenceTextLayer() models.VideoEditorTextLayer {
	return models.VideoEditorTextLayer{ID: uuid.Nil.String(), Text: "x", Start: 0, End: 1, X: .5, Y: .5, FontSize: 48, Color: "#FFFFFF", Align: "center"}
}

// aiApplyTextStyle applies each style group (outline and box colour/size are coupled)
// only when the editor's text validator still accepts the layer.
func aiApplyTextStyle(l models.VideoEditorTextLayer, s *models.EditorAITextStyle) models.VideoEditorTextLayer {
	if s == nil {
		return l
	}
	s = aiTextStyleDefaults(s)
	groups := []func(*models.VideoEditorTextLayer) bool{
		func(c *models.VideoEditorTextLayer) bool { c.Color = s.Color; return s.Color != "" },
		func(c *models.VideoEditorTextLayer) bool {
			c.OutlineColor, c.OutlineWidth = s.OutlineColor, s.OutlineWidth
			return s.OutlineColor != "" || s.OutlineWidth != 0
		},
		func(c *models.VideoEditorTextLayer) bool {
			c.BoxColor, c.BoxOpacity = s.BoxColor, s.BoxOpacity
			return s.BoxColor != "" || s.BoxOpacity != 0
		},
		func(c *models.VideoEditorTextLayer) bool { c.Animation = s.Animation; return s.Animation != "" },
		func(c *models.VideoEditorTextLayer) bool { c.Font = s.Font; return s.Font != "" },
		func(c *models.VideoEditorTextLayer) bool { c.Y = s.Y; return s.Y != 0 },
		func(c *models.VideoEditorTextLayer) bool { c.FontSize = s.FontSize; return s.FontSize != 0 },
	}
	for _, group := range groups {
		candidate := l
		if !group(&candidate) {
			continue
		}
		if _, err := normalizeVETextLayers([]models.VideoEditorTextLayer{candidate}); err == nil {
			l = candidate
		}
	}
	return l
}

// aiTextStyleDefaults mirrors the edit tool: a colour without a size gets the default
// outline width (3) or box opacity (0.6).
func aiTextStyleDefaults(s *models.EditorAITextStyle) *models.EditorAITextStyle {
	out := *s
	if out.OutlineColor != "" && out.OutlineWidth == 0 {
		out.OutlineWidth = 3
	}
	if out.BoxColor != "" && out.BoxOpacity == 0 {
		out.BoxOpacity = .6
	}
	return &out
}

// sanitizeAITextStyle keeps only the fields that survive aiApplyTextStyle.
func sanitizeAITextStyle(s *models.EditorAITextStyle) *models.EditorAITextStyle {
	if s == nil {
		return nil
	}
	s = aiTextStyleDefaults(s)
	got := aiApplyTextStyle(aiReferenceTextLayer(), s)
	out := models.EditorAITextStyle{}
	if s.Color != "" && got.Color == s.Color {
		out.Color = s.Color
	}
	if (s.OutlineColor != "" || s.OutlineWidth != 0) && got.OutlineColor == s.OutlineColor && got.OutlineWidth == s.OutlineWidth {
		out.OutlineColor, out.OutlineWidth = s.OutlineColor, s.OutlineWidth
	}
	if (s.BoxColor != "" || s.BoxOpacity != 0) && got.BoxColor == s.BoxColor && got.BoxOpacity == s.BoxOpacity {
		out.BoxColor, out.BoxOpacity = s.BoxColor, s.BoxOpacity
	}
	if s.Animation != "" && got.Animation == s.Animation {
		out.Animation = s.Animation
	}
	if s.Font != "" && got.Font == s.Font {
		out.Font = s.Font
	}
	if s.Y != 0 && got.Y == s.Y {
		out.Y = s.Y
	}
	if s.FontSize != 0 && got.FontSize == s.FontSize {
		out.FontSize = s.FontSize
	}
	if out == (models.EditorAITextStyle{}) {
		return nil
	}
	return &out
}

func sanitizeAITextStyles(s *models.EditorAITextStyles) *models.EditorAITextStyles {
	if s == nil {
		return nil
	}
	out := models.EditorAITextStyles{Caption: sanitizeAITextStyle(s.Caption), Title: sanitizeAITextStyle(s.Title)}
	if out.Caption == nil && out.Title == nil {
		return nil
	}
	return &out
}

// aiWireTextStyle decodes model numbers leniently so a fractional size drops only that
// field instead of failing the whole plan.
type aiWireTextStyle struct {
	Color        string  `json:"color"`
	OutlineColor string  `json:"outlineColor"`
	OutlineWidth float64 `json:"outlineWidth"`
	BoxColor     string  `json:"boxColor"`
	BoxOpacity   float64 `json:"boxOpacity"`
	Animation    string  `json:"animation"`
	Font         string  `json:"font"`
	Y            float64 `json:"y"`
	FontSize     float64 `json:"fontSize"`
}

func aiWholeInt(v float64) int {
	if !aiFinite(v) || v != math.Trunc(v) || math.Abs(v) > 1e6 {
		return -1
	}
	return int(v)
}

func (w *aiWireTextStyle) style() *models.EditorAITextStyle {
	if w == nil {
		return nil
	}
	return sanitizeAITextStyle(&models.EditorAITextStyle{Color: w.Color, OutlineColor: w.OutlineColor, OutlineWidth: aiWholeInt(w.OutlineWidth), BoxColor: w.BoxColor, BoxOpacity: w.BoxOpacity, Animation: w.Animation, Font: w.Font, Y: w.Y, FontSize: aiWholeInt(w.FontSize)})
}
