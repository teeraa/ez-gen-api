package facade_open_api

import (
	"context"
	"errors"
	"log"
	"strings"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
	"github.com/google/uuid"
)

// characterVoiceResolver validates a voice chosen for a character (implemented by VoiceFacade).
type characterVoiceResolver interface {
	// ResolveCharacterVoice returns nil (no error) when the request removes the voice.
	ResolveCharacterVoice(ctx context.Context, userID string, in req.CharacterVoiceRequest) (*models.CharacterVoice, error)
}

// ResolveCharacterVoice checks a voice for a character: a preset or one of the caller's own voices
// (design or reference), with Voice-menu emotion/speed/pitch, or all of them copied from one of the
// caller's voice generations (templateId). Returns ErrValidation for anything unknown or not owned.
func (f *VoiceFacade) ResolveCharacterVoice(ctx context.Context, userID string, in req.CharacterVoiceRequest) (*models.CharacterVoice, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return nil, ErrUnauthorized
	}
	in.VoiceID, in.TemplateID = strings.TrimSpace(in.VoiceID), strings.TrimSpace(in.TemplateID)
	if in.VoiceID == "" && in.TemplateID == "" {
		return nil, nil
	}
	// normalizeVoiceInput also validates a request id and text; neither is used for a binding.
	v := VoiceInput{RequestID: uuid.NewString(), Text: "-", VoiceID: in.VoiceID, Emotion: in.Emotion, Speed: in.Speed, Pitch: in.Pitch}
	template := ""
	if in.TemplateID != "" {
		tid, e := uuid.Parse(in.TemplateID)
		if e != nil {
			return nil, ErrValidation
		}
		g, e := f.repo.Find(ctx, tid, uid)
		if errors.Is(e, repo.ErrNotFound) {
			return nil, ErrValidation
		} else if e != nil {
			return nil, e
		}
		v.VoiceID, v.Emotion, v.Speed, v.Pitch, template = g.VoiceID, g.Emotion, g.Speed, g.Pitch, g.ID.String()
	}
	n, _, ok := normalizeVoiceInput(v)
	if !ok {
		return nil, ErrValidation
	}
	out := &models.CharacterVoice{VoiceID: n.VoiceID, Emotion: n.Emotion, Speed: n.Speed, Pitch: n.Pitch, TemplateID: template}
	if p, ok := findVoicePreset(n.VoiceID); ok {
		out.Kind, out.VoiceName = "preset", p.Name
		return out, nil
	}
	pid, e := uuid.Parse(n.VoiceID)
	if e != nil {
		return nil, ErrValidation
	}
	p, e := f.repo.FindProfile(ctx, pid, uid)
	if errors.Is(e, repo.ErrNotFound) {
		return nil, ErrValidation
	} else if e != nil {
		return nil, e
	}
	out.Kind, out.VoiceName = p.Kind, p.Name
	return out, nil
}

// characterVoiceView presents a stored voice; nil for none or unreadable JSON.
func characterVoiceView(raw []byte) *dto.CharacterVoiceDTO {
	v := models.DecodeCharacterVoice(raw)
	if v == nil {
		return nil
	}
	out := &dto.CharacterVoiceDTO{VoiceID: v.VoiceID, VoiceName: v.VoiceName, Kind: v.Kind, Emotion: v.Emotion, Speed: v.Speed, Pitch: v.Pitch}
	if v.TemplateID != "" {
		t := v.TemplateID
		out.TemplateID = &t
	}
	return out
}

// SpeakLine synthesizes one line of dialogue in a character's bound voice (film dubbing). It is
// not saved to the Voice-menu history and not charged here: the film shot job carries the cost.
// delivery is a short performance note (e.g. "low, gruff") added to the voice description.
// Returns ErrValidation when the bound voice no longer exists (e.g. a deleted profile).
func (f *VoiceFacade) SpeakLine(ctx context.Context, userID string, v models.CharacterVoice, text, delivery string) ([]byte, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if !f.available() {
		return nil, ErrVoiceUnavailable
	}
	in := VoiceInput{RequestID: uuid.NewString(), Text: text, VoiceID: v.VoiceID, Emotion: v.Emotion, Speed: v.Speed, Pitch: v.Pitch}
	in, _, ok := normalizeVoiceInput(in)
	if !ok {
		return nil, ErrValidation
	}
	req, _, _, e := f.resolveVoice(ctx, uid, in)
	if e != nil {
		return nil, e
	}
	if d := strings.TrimSpace(delivery); d != "" {
		// Stays in the instruction part of the prompt, which the model does not read aloud.
		req.TextPrompt = "Performance (do not read aloud): " + directorTruncate(d, 360) + ".\n" + req.TextPrompt
	}
	out, e := f.gen.Generate(ctx, req)
	if e != nil {
		log.Printf("voice: user=%s film line failed: %v", uid, e)
		switch {
		case errors.Is(e, seedaudio.ErrNotGranted):
			return nil, ErrVoiceNotGranted
		case errors.Is(e, seedaudio.ErrRejected):
			return nil, ErrVoiceRejected
		}
		return nil, ErrVoiceFailed
	}
	if len(out.Audio) == 0 {
		return nil, ErrVoiceFailed
	}
	return out.Audio, nil
}
