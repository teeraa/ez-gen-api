package facade_open_api

import (
	"context"
	"errors"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

func TestResolveCharacterVoice(t *testing.T) {
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	design := models.VoiceProfile{Base: models.Base{ID: uuid.New()}, UserID: owner, Name: "เสียงพระเอก", Kind: "design", Description: "deep calm Thai man"}
	foreign := models.VoiceProfile{Base: models.Base{ID: uuid.New()}, UserID: other, Name: "x", Kind: "design", Description: "someone else"}
	tmpl := models.VoiceGeneration{Base: models.Base{ID: uuid.New()}, UserID: owner, VoiceID: design.ID.String(), Emotion: "อบอุ่น", Speed: 1.2, Pitch: -2}
	f := NewVoiceFacade(&voiceRepoFake{profiles: []models.VoiceProfile{design, foreign}, rows: []models.VoiceGeneration{tmpl}}, storage.NewMemoryClient(), nil)
	user := owner.String()

	if v, e := f.ResolveCharacterVoice(ctx, user, req.CharacterVoiceRequest{}); e != nil || v != nil {
		t.Fatalf("empty request must remove the voice: %+v %v", v, e)
	}
	v, e := f.ResolveCharacterVoice(ctx, user, req.CharacterVoiceRequest{VoiceID: "nara"})
	if e != nil || v.Kind != "preset" || v.VoiceName != "นารา" || v.Emotion != "ปกติ" || v.Speed != 1 {
		t.Fatalf("preset: %+v %v", v, e)
	}
	v, e = f.ResolveCharacterVoice(ctx, user, req.CharacterVoiceRequest{TemplateID: tmpl.ID.String(), VoiceID: "krit"})
	if e != nil || v.VoiceID != design.ID.String() || v.Kind != "design" || v.Emotion != "อบอุ่น" || v.Speed != 1.2 || v.Pitch != -2 || v.TemplateID != tmpl.ID.String() {
		t.Fatalf("template must win and copy its settings: %+v %v", v, e)
	}
	for name, in := range map[string]req.CharacterVoiceRequest{
		"unknown preset":   {VoiceID: "nobody"},
		"foreign profile":  {VoiceID: foreign.ID.String()},
		"foreign template": {TemplateID: uuid.NewString()},
		"bad emotion":      {VoiceID: "nara", Emotion: "โกรธจัด"},
		"bad speed":        {VoiceID: "nara", Speed: 3},
	} {
		if _, e := f.ResolveCharacterVoice(ctx, user, in); !errors.Is(e, ErrValidation) {
			t.Fatalf("%s: want ErrValidation, got %v", name, e)
		}
	}
	if got := characterVoiceView(models.EncodeCharacterVoice(nil)); got != nil {
		t.Fatalf("no voice must present as null: %+v", got)
	}
}
