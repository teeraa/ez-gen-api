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

	withVoice, withLines := filmShotHash(sh, byID, "cinematic"), filmLinesHash(filmVoiceLines(sh, byID))
	byID[voiced.ID.String()].Voice = nil
	without, withoutLines := filmShotHash(sh, byID, "cinematic"), filmLinesHash(filmVoiceLines(sh, byID))
	if withVoice != without || withLines == withoutLines || withoutLines != filmNoVoices {
		t.Fatal("a voice changes the dubbed lines, never the video hash")
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

func TestFilmLineEmotionAndSceneReachTheVoice(t *testing.T) {
	hero := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "มาร์คัส",
		Voice: models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: "tawan", VoiceName: "ตะวัน", Kind: "preset", Emotion: "ปกติ", Speed: 1, TemplateID: "t"})}
	byID := filmElementMap([]models.FilmElement{hero})
	id := hero.ID.String()
	sh := &filmShot{ID: "s", Seconds: 6, CharacterIDs: []string{id}, Action: "Marcus raises his sword and charges",
		Dialogue: []filmLine{{SpeakerID: id, Line: "เดินหน้า!", Delivery: "battle cry", Emotion: "ตะโกน"}, {SpeakerID: id, Line: "ไปกันเถอะ"}}}
	lines := filmVoiceLines(sh, byID)
	if lines[0].Voice.Emotion != "ตะโกน" || lines[0].Voice.TemplateID != "" || lines[1].Voice.Emotion != "ปกติ" {
		t.Fatalf("a line's emotion overrides the character's: %+v", lines)
	}
	if p := lines[0].performance(); !strings.Contains(p, "battle cry") || !strings.Contains(p, "Scene: Marcus raises") {
		t.Fatalf("the voice hears the delivery and the shot's moment: %q", p)
	}
	if !strings.Contains(filmPlanRulesText(), "ตะโกน") || strings.Contains(filmPlanRulesText(), "%EMOTIONS%") {
		t.Fatal("the planner picks line emotions from the voice list")
	}
	b := &filmBrief{Shot: sh, Cast: []*models.FilmElement{byID[id]}, Style: "cinematic", Dubbed: map[string]bool{id: true}, AudioRef: true}
	var w strings.Builder
	filmPerformance(b, &w)
	if p := w.String(); !strings.Contains(p, "Lip-sync to audio 1") || !strings.Contains(p, "1. @มาร์คัส") || strings.Contains(p, "generate NO voice") {
		t.Fatalf("voice-first prompt syncs lips to audio 1:\n%s", p)
	}
}
