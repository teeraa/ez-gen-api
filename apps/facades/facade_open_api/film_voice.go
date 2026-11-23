package facade_open_api

// Film dubbing: dialogue of characters with a bound Voice-menu voice is generated with seed-audio
// and mixed into the shot clip right after the video provider returns it, so the stored clip,
// the shot preview and the export all carry the character's own voice (one flow, no extra step).
// The video prompt asks the provider to animate those lines without voicing them.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

const (
	// filmDubLead is where the first dubbed line starts; filmDubGap separates lines.
	filmDubLead = 0.4
	filmDubGap  = 0.25
	// filmDubClipVolume keeps the provider's ambience/effects under the dubbed speech.
	filmDubClipVolume = 0.55
	// filmDubRunesPerSecond estimates Thai speech length for pricing before the audio exists.
	filmDubRunesPerSecond = 12.0
	// filmDubRetryWindow bounds retries of a failing dub (measured from video submission).
	filmDubRetryWindow = 30 * time.Minute
)

// filmVoiceService validates and speaks character voices (implemented by VoiceFacade).
type filmVoiceService interface {
	characterVoiceResolver
	SpeakLine(ctx context.Context, userID string, v models.CharacterVoice, text, delivery string) ([]byte, error)
}

// filmVoiceLine is one dubbed line frozen into a shot job.
type filmVoiceLine struct {
	SpeakerID string                `json:"speakerId"`
	Text      string                `json:"text"`
	Delivery  string                `json:"delivery,omitempty"`
	Voice     models.CharacterVoice `json:"voice"`
}

// filmBoundVoice is the voice of a character element, or nil.
func filmBoundVoice(el *models.FilmElement) *models.CharacterVoice {
	if el == nil || el.Kind != "character" {
		return nil
	}
	return models.DecodeCharacterVoice(el.Voice)
}

// filmVoiceRequired is returned when a shot's character has no voice yet.
func filmVoiceRequired(name string) error {
	return &DirectorError{Status: 422, Code: "VOICE_REQUIRED", Message: "ตัวละคร “" + name + "” ยังไม่มีเสียง กรุณาเลือกเสียงในขั้นองค์ประกอบก่อน"}
}

// filmPlanVoiceGuide lists the preset voices the planner may give a new character.
func filmPlanVoiceGuide() string {
	var b strings.Builder
	for _, p := range voicePresets {
		fmt.Fprintf(&b, "%s (%s), ", p.ID, p.Prompt)
	}
	return strings.TrimSuffix(b.String(), ", ")
}

// filmPlanVoice turns the planner's preset pick into a stored voice; nil for an unknown id.
func filmPlanVoice(id string) []byte {
	p, ok := findVoicePreset(strings.ToLower(strings.TrimSpace(id)))
	if !ok {
		return nil
	}
	return models.EncodeCharacterVoice(&models.CharacterVoice{VoiceID: p.ID, VoiceName: p.Name, Kind: "preset", Emotion: "ปกติ", Speed: 1})
}

// filmVoiceLines lists the shot's dialogue spoken by characters that have a bound voice.
func filmVoiceLines(sh *filmShot, byID map[string]*models.FilmElement) []filmVoiceLine {
	out := []filmVoiceLine{}
	for _, l := range sh.Dialogue {
		if v := filmBoundVoice(byID[l.SpeakerID]); v != nil && l.Line != "" {
			out = append(out, filmVoiceLine{SpeakerID: l.SpeakerID, Text: l.Line, Delivery: l.Delivery, Voice: *v})
		}
	}
	return out
}

// filmDubLines are the lines a job dubs; none when dubbing is not configured.
func (f *FilmFacade) filmDubLines(sh *filmShot, byID map[string]*models.FilmElement) []filmVoiceLine {
	if f.voices == nil || f.options.Processor == nil {
		return nil
	}
	return filmVoiceLines(sh, byID)
}

// filmVoicePoints prices dubbing at 1 point per started 10 seconds of estimated speech.
func filmVoicePoints(lines []filmVoiceLine) int {
	if len(lines) == 0 {
		return 0
	}
	runes := 0
	for _, l := range lines {
		runes += utf8.RuneCountInString(l.Text)
	}
	return max(1, int(math.Ceil(float64(runes)/filmDubRunesPerSecond/10)))
}

// filmVoiceHash is the combined shot+voices hash clips carried before voices were kept apart from
// the video hash; it only recognises those legacy clips now (filmVideoFresh, filmVoiceStatus).
func filmVoiceHash(base string, lines []filmVoiceLine) string {
	if len(lines) == 0 {
		return base
	}
	return storyHash(struct {
		Base   string
		Voices []filmVoiceLine
	}{base, lines})
}

// errFilmDubRetry marks a dubbing failure worth retrying (provider or storage hiccup).
var errFilmDubRetry = errors.New("film dubbing temporarily failed")

// dubClip mixes the job's voice lines over the clip. Lines whose voice no longer exists are skipped.
func (f *FilmFacade) dubClip(c context.Context, user string, in *filmVideoJob, clip []byte) ([]byte, error) {
	if len(in.Voices) == 0 {
		return clip, nil
	}
	if f.voices == nil || f.options.Processor == nil {
		return clip, nil
	}
	dir, e := os.MkdirTemp("", "film-dub-*")
	if e != nil {
		return nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
	}
	defer os.RemoveAll(dir)
	clipPath := filepath.Join(dir, "clip.mp4")
	if e = os.WriteFile(clipPath, clip, 0o600); e != nil {
		return nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
	}
	tracks := []videoprocessing.AudioTrack{}
	at := filmDubLead
	for i, l := range in.Voices {
		audio, e := f.voices.SpeakLine(c, user, l.Voice, l.Text, l.Delivery)
		if errors.Is(e, ErrValidation) {
			log.Printf("film: shot=%s line %d skipped, voice %s no longer available", in.ShotID, i, l.Voice.VoiceID)
			continue
		}
		if errors.Is(e, ErrVoiceRejected) || errors.Is(e, ErrVoiceNotGranted) || errors.Is(e, ErrVoiceUnavailable) {
			return nil, e
		}
		if e != nil {
			return nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
		}
		path := filepath.Join(dir, fmt.Sprintf("line-%02d.mp3", i))
		if e = os.WriteFile(path, audio, 0o600); e != nil {
			return nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
		}
		d, e := videoprocessing.AudioDuration(c, path)
		if e != nil {
			return nil, fmt.Errorf("%w: line %d: %v", errFilmDubRetry, i, e)
		}
		tracks = append(tracks, videoprocessing.AudioTrack{Path: path, TrimEnd: d, At: at, Volume: 1, FadeOut: math.Min(0.08, d/4)})
		at += d + filmDubGap
	}
	if len(tracks) == 0 {
		return clip, nil
	}
	if at-filmDubGap > float64(in.Seconds)+0.05 {
		log.Printf("film: shot=%s dubbed lines run %.1fs, longer than the %ds clip; the tail is cut", in.ShotID, at-filmDubGap, in.Seconds)
	}
	resolution := "720p"
	if in.Resolution == "1080p" && !in.Draft {
		resolution = "1080p"
	}
	volume := filmDubClipVolume
	out, e := f.options.Processor.Merge(c, videoprocessing.MergeRequest{
		Clips: []videoprocessing.Clip{{Path: clipPath, Volume: &volume}}, OutputResolution: resolution, OutputAspectRatio: in.Ratio,
		AudioVolume: 1, AudioTracks: tracks,
	})
	if e != nil || len(out) == 0 {
		return nil, fmt.Errorf("%w: mix: %v", errFilmDubRetry, e)
	}
	return out, nil
}
