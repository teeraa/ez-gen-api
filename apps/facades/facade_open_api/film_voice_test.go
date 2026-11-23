package facade_open_api

import (
	"errors"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

func TestFilmVoiceLinesPointsAndHash(t *testing.T) {
	voiced := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "มาร์คัส",
		Voice: models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: "tawan", VoiceName: "ตะวัน", Kind: "preset", Emotion: "ปกติ", Speed: 1})}
	plain := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "ลูเซียส"}
	byID := filmElementMap([]models.FilmElement{voiced, plain})
	sh := &filmShot{ID: "s1", Seconds: 8, CharacterIDs: []string{voiced.ID.String(), plain.ID.String()}, Action: "they talk",
		Dialogue: []filmLine{{SpeakerID: voiced.ID.String(), Line: "อดทนไว้ อีกไม่นานเราจะได้กลับบ้าน", Delivery: "low"}, {SpeakerID: plain.ID.String(), Line: "ครับ"}}}

	lines := filmVoiceLines(sh, byID)
	if len(lines) != 1 || lines[0].Voice.VoiceID != "tawan" || lines[0].Delivery != "low" {
		t.Fatalf("only the voiced speaker is dubbed: %+v", lines)
	}
	if p := filmVoicePoints(lines); p != 1 {
		t.Fatalf("a short line costs 1 point, got %d", p)
	}
	if filmVoicePoints(nil) != 0 {
		t.Fatal("no dubbing costs nothing")
	}

	withVoice := filmShotHash(sh, byID, "cinematic")
	byID[voiced.ID.String()].Voice = nil
	without := filmShotHash(sh, byID, "cinematic")
	if withVoice == without {
		t.Fatal("binding a voice must make the shot stale")
	}
	if filmVoiceHash("base", nil) != "base" {
		t.Fatal("shots without voiced lines keep their old hash")
	}

	b := &filmBrief{Shot: sh, Cast: []*models.FilmElement{&voiced, &plain}, Style: "cinematic", Dubbed: map[string]bool{voiced.ID.String(): true}}
	var w strings.Builder
	filmPerformance(b, &w)
	prompt := w.String()
	if !strings.Contains(prompt, "generate NO voice for @มาร์คัส") || !strings.Contains(prompt, "@ลูเซียส speaks in Thai") {
		t.Fatalf("dubbed speaker must be silent, the other still speaks:\n%s", prompt)
	}
}

func TestFilmPlanVoicePicksPresetsOnly(t *testing.T) {
	v := models.DecodeCharacterVoice(filmPlanVoice(" Krit "))
	if v == nil || v.VoiceID != "krit" || v.Kind != "preset" || v.VoiceName == "" || v.Speed != 1 || v.Emotion != "ปกติ" {
		t.Fatalf("a preset id becomes a stored preset voice: %+v", v)
	}
	for _, id := range []string{"", "unknown", uuid.NewString()} {
		if filmPlanVoice(id) != nil {
			t.Fatalf("%q is not a preset and must not be bound", id)
		}
	}
	rules := filmPlanRulesText()
	if strings.Contains(rules, "%VOICES%") || !strings.Contains(rules, "nara (") {
		t.Fatal("the planner rules list the preset voices")
	}
	var de *DirectorError
	if e := filmVoiceRequired("ขุนอินทร์"); !errors.As(e, &de) || de.Status != 422 || de.Code != "VOICE_REQUIRED" || !strings.Contains(de.Message, "ขุนอินทร์") {
		t.Fatalf("missing voice error: %v", e)
	}
}
