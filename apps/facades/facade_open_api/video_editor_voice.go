package facade_open_api

// AI editor voice generation: text (plus an optional voice description or BytePlus speaker ID) is
// synthesized by Seed Audio and stored as a normal audio asset of the project, so it appears in the
// audio library and can be placed on an audio track like an uploaded file.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
)

var (
	// ErrVEVoiceUnavailable: no Seed Audio API key configured.
	ErrVEVoiceUnavailable = errors.New("video editor voice generation not configured")
	// ErrVEVoiceRejected: the provider refused the text or voice (policy or invalid input).
	ErrVEVoiceRejected = errors.New("video editor voice generation rejected")
	// ErrVEVoiceFailed: the provider failed; the same request may succeed later.
	ErrVEVoiceFailed = errors.New("video editor voice generation failed")
)

// VoiceRequest is the body of POST /video-editor/projects/:id/voice.
type VoiceRequest struct {
	ClientUploadID string `json:"clientUploadId"`
	Text           string `json:"text"`
	// Voice describes the voice in natural language, e.g. "ผู้หญิงวัยรุ่น เสียงสดใส".
	Voice string `json:"voice,omitempty"`
	// Speaker is a BytePlus voice ID (TTS 2.0 voice list or a cloned voice).
	Speaker    string `json:"speaker,omitempty"`
	SpeechRate int    `json:"speechRate,omitempty"`
	PitchRate  int    `json:"pitchRate,omitempty"`
}

// WithVoice enables voice generation.
func (f *VideoEditorFacade) WithVoice(c *seedaudio.Client) *VideoEditorFacade {
	f.voice = c
	return f
}

func (f *VideoEditorFacade) VoiceAvailable() bool { return f.voice != nil }

// voicePrompt keeps the text to speak separate from the description so the description is not read aloud.
func voicePrompt(in VoiceRequest) string {
	text := strings.TrimSpace(in.Text)
	switch {
	case in.Speaker != "":
		return "@Audio1 reads the following text exactly, word for word:\n" + text
	case strings.TrimSpace(in.Voice) != "":
		return fmt.Sprintf("Voice: %s.\nSpeak the following text exactly, word for word, in its own language. Do not read the voice description aloud.\n%s", strings.TrimSpace(in.Voice), text)
	default:
		return text
	}
}

func voiceAssetName(text string) string {
	name := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(name) > 24 {
		name = string([]rune(name)[:24]) + "…"
	}
	return "เสียงพูด · " + name + ".mp3"
}

func validVoice(in VoiceRequest) bool {
	text := strings.TrimSpace(in.Text)
	return veID(in.ClientUploadID) && text != "" && utf8.RuneCountInString(voicePrompt(in)) <= seedaudio.MaxPromptRunes &&
		utf8.RuneCountInString(in.Voice) <= 200 && len(in.Speaker) <= 120 &&
		in.SpeechRate >= -50 && in.SpeechRate <= 100 && in.PitchRate >= -12 && in.PitchRate <= 12
}

// GenerateVoice synthesizes speech and stores it as an audio asset of the project.
func (f *VideoEditorFacade) GenerateVoice(ctx context.Context, id, user string, in VoiceRequest) (dto.VideoEditorVoiceDTO, bool, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorVoiceDTO{}, false, repo.ErrNotFound
	}
	if f.voice == nil {
		return dto.VideoEditorVoiceDTO{}, false, ErrVEVoiceUnavailable
	}
	if !validVoice(in) {
		return dto.VideoEditorVoiceDTO{}, false, ErrVEInvalid
	}
	if _, e := f.repo.Project(ctx, id, user); e != nil {
		return dto.VideoEditorVoiceDTO{}, false, e
	}
	req := seedaudio.Request{TextPrompt: voicePrompt(in), Audio: seedaudio.AudioConfig{Format: "mp3", SpeechRate: in.SpeechRate, PitchRate: in.PitchRate, EnableSubtitle: true}}
	if in.Speaker != "" {
		req.References = []seedaudio.Reference{{Speaker: in.Speaker}}
	}
	// Charged per client upload ID; refunded unless this call adds a new asset.
	pointRef := user + ":" + in.ClientUploadID
	paid, e := chargePointsNew(ctx, user, pointFeatureEditorVoice, pointRef, pointPrices.Voice)
	if e != nil {
		return dto.VideoEditorVoiceDTO{}, false, e
	}
	added := false
	defer func() {
		if paid && !added {
			refundPoints(ctx, user, pointFeatureEditorVoice, pointRef)
		}
	}()
	out, e := f.voice.Generate(ctx, req)
	if e != nil {
		log.Printf("video_editor: voice project=%s failed: %v", id, e)
		if errors.Is(e, seedaudio.ErrNotGranted) {
			return dto.VideoEditorVoiceDTO{}, false, ErrVEVoiceUnavailable
		}
		if errors.Is(e, seedaudio.ErrRejected) {
			return dto.VideoEditorVoiceDTO{}, false, ErrVEVoiceRejected
		}
		return dto.VideoEditorVoiceDTO{}, false, ErrVEVoiceFailed
	}
	asset, created, e := f.Upload(ctx, id, user, in.ClientUploadID, voiceAssetName(in.Text), "audio/mpeg", out.Audio)
	if e != nil {
		return dto.VideoEditorVoiceDTO{}, false, e
	}
	added = created
	result := dto.VideoEditorVoiceDTO{Asset: asset, Sentences: []dto.VideoEditorVoiceSentence{}}
	for _, s := range out.Sentences {
		result.Sentences = append(result.Sentences, dto.VideoEditorVoiceSentence{Start: float64(s.StartMS) / 1000, End: float64(s.EndMS) / 1000, Text: s.Text})
	}
	return result, created, nil
}
