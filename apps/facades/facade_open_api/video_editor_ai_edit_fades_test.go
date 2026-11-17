package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// "เพิ่มเฟดและทรานซิชัน": models send crossfade names, index targets and a transition on
// the first clip in the same call as its fade; none of that should sink the whole edit.
func TestAIEditFadesAndTransitionsTolerateModelHabits(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	base.Segments[1].VideoEditorTransition = models.VideoEditorTransition{}
	result := aiEditResult{Operations: []aiEditOp{
		{Tool: "set_segment", Target: base.Segments[0].ID, FadeIn: aiEditF(.5), Transition: aiEditS("crossfade"), TransitionDuration: aiEditF(.5)},
		{Tool: "set_segment", Target: "1", FadeOut: aiEditF(.5), Transition: aiEditS("Cross Dissolve"), TransitionDuration: aiEditF(.5)},
		{Tool: "set_segment", Target: base.Segments[0].ID, Transition: aiEditS("fade")},
	}}
	got, applied, rejected := applyAIEditResult(base, result, assets, true)
	if len(applied) != 2 || rejected != 1 {
		t.Fatalf("applied %+v rejected %d", applied, rejected)
	}
	first, second := got.Segments[0], got.Segments[1]
	if first.FadeIn != .5 || first.Transition != "" || second.Transition != "fade" || second.TransitionDuration != .5 || second.FadeOut != .5 {
		t.Fatalf("segments %+v", got.Segments)
	}
	if applied[1].Target != base.Segments[1].ID {
		t.Fatalf("index target not resolved to id: %+v", applied[1])
	}
	for in, want := range map[string]string{"dissolve": "fade", "fade_to_black": "fadeblack", "Dip to White": "fadewhite", "wipeleft": "wipeleft", "zoom": "zoom"} {
		if got := aiEditTransitionName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestAIEditNoChangesCarriesModelReasonOnlyWithoutOperations(t *testing.T) {
	t.Parallel()
	base, assets := aiEditTestState(t)
	raw := func(v any) datatypes.JSON { b, _ := json.Marshal(v); return b }
	p := &models.VideoEditorProject{Base: models.Base{ID: uuid.New()}, Title: "งาน", AspectRatio: "9:16", Revision: 1, Segments: raw(base.Segments), AudioClips: raw(base.AudioClips), TextLayers: raw(base.TextLayers), ImageLayers: raw(base.ImageLayers)}
	ctx := context.Background()
	_, err := previewAIEdit(ctx, &aiEditFakeProvider{out: []byte(`{"summary":"ยังไม่มีซับไตเติ้ลในไทม์ไลน์","operations":[]}`)}, p, assets, "x", true, nil)
	var reason *AIEditNoChangesError
	if !errors.Is(err, ErrAIEditNoChanges) || !errors.As(err, &reason) || reason.Reason != "ยังไม่มีซับไตเติ้ลในไทม์ไลน์" {
		t.Fatalf("no-op reason: %v", err)
	}
	_, err = previewAIEdit(ctx, &aiEditFakeProvider{out: []byte(`{"summary":"ใส่เฟดแล้ว","operations":[{"tool":"set_segment","target":"missing","description":"","params":"{\"fadeIn\":1}"}]}`)}, p, assets, "x", true, nil)
	if !errors.Is(err, ErrAIEditNoChanges) || errors.As(err, &reason) {
		t.Fatalf("rejected ops must not forward the model summary: %v", err)
	}
}
