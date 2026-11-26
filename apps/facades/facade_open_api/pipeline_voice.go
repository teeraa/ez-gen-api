package facade_open_api

// สร้างวิดีโอ (studio pipeline) dubbing, done the way films dub (voice first, film_voice.go):
// the dialogue is spoken with the chosen Voice-menu voice (seed-audio) before the video, the
// line is sent to the video model as reference audio 1 so the lips follow it, and once the clip
// returns the clean line is mixed over it with the clip's own audio muted and a quiet room tone
// underneath. Stages: [image → save_character →] voice → video → dub → save_video.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
)

// pipelineVoiceService resolves and speaks Voice-menu voices (implemented by VoiceFacade).
type pipelineVoiceService interface {
	characterVoiceResolver
	SpeakLine(ctx context.Context, userID string, v models.CharacterVoice, text, delivery string) ([]byte, error)
	// available reports whether seed-audio is configured (BYTEPLUS_VOICE_API_KEY).
	available() bool
}

// PipelineVoiceOptions turns on dubbing for the studio pipeline. All fields are required.
type PipelineVoiceOptions struct {
	Voices    pipelineVoiceService
	Processor videoprocessing.VideoProcessor
	// FetchVideo and FetchAudio download stored media from a signed URL.
	FetchVideo func(context.Context, string) ([]byte, string, error)
	FetchAudio func(context.Context, string) ([]byte, string, error)
}

// WithVoice enables dubbing pipeline dialogue with a Voice-menu voice.
func (f *PipelineFacade) WithVoice(o PipelineVoiceOptions) *PipelineFacade {
	f.voice = o
	return f
}

func (f *PipelineFacade) voiceAvailable() bool {
	o := f.voice
	return o.Voices != nil && o.Voices.available() && o.Processor != nil && o.FetchVideo != nil && o.FetchAudio != nil && f.storage != nil
}

// pipelineVoiceModel reports whether a video model can lip-sync to reference audio
// (Seedance 2.x with native audio and reference images).
func pipelineVoiceModel(m modelark.ModelConfig) bool {
	return m.SupportsAudio && m.SupportsRefImage && strings.Contains(m.ProviderModelID, "seedance-2")
}

// pipelineWantsVoice is true when the request names a voice (an empty object means none).
func pipelineWantsVoice(v *models.CharacterVoice) bool { return v != nil && v.VoiceID != "" }

// pipelineVoiceState is stored in studio_pipelines.voice.
type pipelineVoiceState struct {
	// Attempt numbers voice generations; a failed one is refunded and the next retry charges anew.
	Attempt int `json:"attempt,omitempty"`
	// LineKey is the treated line (MP3); At and Seconds place it in the clip.
	LineKey string  `json:"lineKey,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	At      float64 `json:"at,omitempty"`
	// TrackKey is the dialogue track (the line at its time on silence as long as the clip),
	// sent to the video model as reference audio.
	TrackKey string `json:"trackKey,omitempty"`
	// DubbedKey is the finished clip with the line mixed in; it becomes the Library video.
	DubbedKey string `json:"dubbedKey,omitempty"`
}

func pipelineVoiceRead(p *models.StudioPipeline) pipelineVoiceState {
	var s pipelineVoiceState
	if len(p.Voice) > 0 {
		_ = json.Unmarshal(p.Voice, &s)
	}
	return s
}

func pipelineVoiceWrite(p *models.StudioPipeline, s pipelineVoiceState) {
	p.Voice, _ = json.Marshal(s)
}

// pipelineVoiceFail stops the pipeline at its current stage; the user can retry it.
func pipelineVoiceFail(p *models.StudioPipeline, code, message string) error {
	p.Status = "failed"
	p.FailureCode = &code
	p.FailureMessage = &message
	return nil
}

// pipelineVoiceNote is the performance note the voice model gets (never read aloud).
func pipelineVoiceNote(scene string) string {
	note := "On-camera dialogue in a live-action scene: natural and unhurried, real breaths, spoken to someone in the scene, never a narrator, announcer or advertisement read. No music."
	if s := strings.TrimSpace(scene); s != "" {
		note += " Scene: " + directorTruncate(s, 220)
	}
	return note
}

// speakVoice is the voice stage: it charges the voice, speaks and fits the line to the clip,
// stores the line and the dialogue track, and moves on to the video.
func (f *PipelineFacade) speakVoice(c context.Context, p *models.StudioPipeline, frozen pipelineSnapshot) error {
	st := pipelineVoiceRead(p)
	if st.TrackKey != "" {
		p.Stage = "video"
		return nil
	}
	if !f.voiceAvailable() {
		return pipelineVoiceFail(p, "VOICE_UNAVAILABLE", "ระบบเสียงพากย์ยังไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง")
	}
	in, user := frozen.Request, p.UserID.String()
	ref := pipelineKey(p, fmt.Sprintf("voice:%d", st.Attempt))
	if e := chargePoints(c, user, pointFeatureVoice, ref, pointPrices.Voice); e != nil {
		return e
	}
	fail := func(code, message string, cause error) error {
		log.Printf("pipeline: id=%s voice attempt %d failed: %v", p.ID, st.Attempt, cause)
		refundPoints(context.WithoutCancel(c), user, pointFeatureVoice, ref)
		st.Attempt++
		pipelineVoiceWrite(p, st)
		return pipelineVoiceFail(p, code, message)
	}
	dir, e := os.MkdirTemp("", "pipeline-voice-*")
	if e != nil {
		return fail("VOICE_FAILED", "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", e)
	}
	defer os.RemoveAll(dir)
	path, seconds, at, e := f.speakFitted(c, user, *frozen.Voice, in.Dialogue, pipelineVoiceNote(in.ScenePrompt), in.DurationSeconds, dir)
	switch {
	case errors.Is(e, ErrValidation):
		return fail("VOICE_NOT_FOUND", "ไม่พบเสียงที่เลือก (อาจถูกลบไปแล้ว) กรุณาเลือกเสียงใหม่แล้วสร้างคำขอใหม่", e)
	case errors.Is(e, ErrVoiceRejected):
		return fail("VOICE_REJECTED", "ระบบเสียงไม่รับบทพูดหรือเสียงนี้ กรุณาปรับบทพูดหรือเปลี่ยนเสียงแล้วสร้างคำขอใหม่", e)
	case errors.Is(e, ErrVoiceNotGranted), errors.Is(e, ErrVoiceUnavailable):
		return fail("VOICE_UNAVAILABLE", "ระบบเสียงพากย์ยังไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง", e)
	case e != nil:
		return fail("VOICE_FAILED", "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", e)
	}
	line, e := os.ReadFile(path)
	if e != nil {
		return fail("VOICE_FAILED", "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", e)
	}
	clip := float64(in.DurationSeconds)
	track, e := videoprocessing.MixAudio(c, []videoprocessing.AudioTrack{{Path: path, TrimEnd: math.Min(seconds, clip-at), At: at, Volume: 1}}, clip)
	if e != nil {
		return fail("VOICE_FAILED", "สร้างเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", fmt.Errorf("dialogue track: %w", e))
	}
	prefix := fmt.Sprintf("studio-pipelines/%s/%s/voice-%d", user, p.ID, st.Attempt)
	if e = f.storage.Upload(c, prefix+"-line.mp3", line, "audio/mpeg"); e == nil {
		e = f.storage.Upload(c, prefix+"-dialogue.wav", track, "audio/wav")
	}
	if e != nil {
		return fail("VOICE_FAILED", "บันทึกเสียงพากย์ไม่สำเร็จ กรุณาลองใหม่", e)
	}
	st.LineKey, st.Seconds, st.At, st.TrackKey = prefix+"-line.mp3", seconds, at, prefix+"-dialogue.wav"
	pipelineVoiceWrite(p, st)
	p.Stage = "video"
	return nil
}

// speakFitted speaks the line and gives it the production-sound treatment, placed from filmDubLead.
// A line that would run past the clip is played faster (up to filmDubMaxTempo, pitch kept) with a
// shorter lead; if it is still too long one brisk take is made and the shorter one kept, as films do.
// It returns the treated file, its length and its start in the clip.
func (f *PipelineFacade) speakFitted(c context.Context, user string, v models.CharacterVoice, text, note string, clip int, dir string) (string, float64, float64, error) {
	take := func(name string, v models.CharacterVoice, note string) (string, error) {
		audio, e := f.voice.Voices.SpeakLine(c, user, v, text, note)
		if e != nil {
			return "", e
		}
		raw := filepath.Join(dir, name+"-raw.mp3")
		return raw, os.WriteFile(raw, audio, 0o600)
	}
	treat := func(raw, name string, tempo float64) (string, float64, error) {
		out := filepath.Join(dir, name+".mp3")
		if e := videoprocessing.TreatDialogueFile(c, raw, out, tempo); e != nil {
			// Without ffmpeg filters the raw take is used as it is.
			log.Printf("pipeline: dialogue treatment skipped: %v", e)
			data, e := os.ReadFile(raw)
			if e != nil {
				return "", 0, e
			}
			if e = os.WriteFile(out, data, 0o600); e != nil {
				return "", 0, e
			}
		}
		d, e := videoprocessing.AudioDuration(c, out)
		return out, math.Round(d*1000) / 1000, e
	}
	budget := float64(clip) - 0.3
	raw, e := take("line", v, note)
	if e != nil {
		return "", 0, 0, e
	}
	path, d, e := treat(raw, "line", 1)
	if e != nil || filmDubLead+d <= budget {
		return path, d, filmDubLead, e
	}
	const lead = 0.15
	squeeze := func(raw, name string, d float64) (string, float64, error) {
		tempo := filmDubMaxTempo
		if room := budget - lead; room > 0 {
			tempo = math.Max(1, math.Min(filmDubMaxTempo, d/room*1.02))
		}
		return treat(raw, name, tempo)
	}
	if path, d, e = squeeze(raw, "line-tempo", d); e != nil || lead+d <= budget {
		return path, d, lead, e
	}
	brisk := v
	if brisk.Speed <= 0 {
		brisk.Speed = 1
	}
	brisk.Speed = math.Min(2, brisk.Speed*1.2)
	if fastRaw, e := take("fast", brisk, filmDubPace+" "+note); e == nil {
		if fast, fd, e := treat(fastRaw, "fast", 1); e == nil {
			if lead+fd > budget {
				fast, fd, e = squeeze(fastRaw, "fast-tempo", fd)
			}
			if e == nil && fd < d {
				path, d = fast, fd
			}
		}
	}
	if lead+d > budget {
		log.Printf("pipeline: dubbed line runs %.1fs in a %ds clip; the tail is cut", lead+d, clip)
	}
	return path, d, lead, nil
}

// dubVideo is the dub stage: the stored line is mixed over the finished clip (clip audio muted,
// room tone under it) and the result is stored for the Library.
func (f *PipelineFacade) dubVideo(c context.Context, p *models.StudioPipeline, frozen pipelineSnapshot) error {
	st := pipelineVoiceRead(p)
	if st.DubbedKey != "" {
		p.Stage = "save_video"
		return nil
	}
	fail := func(e error) error {
		log.Printf("pipeline: id=%s dub failed: %v", p.ID, e)
		return pipelineVoiceFail(p, "DUB_FAILED", "ผสมเสียงพากย์ลงวิดีโอไม่สำเร็จ กดลองอีกครั้งเพื่อผสมใหม่ (ไม่สร้างวิดีโอใหม่)")
	}
	if !f.voiceAvailable() || st.LineKey == "" || p.VideoJobID == nil {
		return fail(errors.New("dubbing not configured or no line"))
	}
	user := p.UserID.String()
	j, e := f.reader.FindByID(c, p.VideoJobID.String(), user)
	if e != nil {
		return e
	}
	if j.Status != models.JobStatusSucceeded || j.OutputMediaKey == nil || !durableKey(*j.OutputMediaKey) {
		return fail(errors.New("video output not stored"))
	}
	dir, e := os.MkdirTemp("", "pipeline-dub-*")
	if e != nil {
		return fail(e)
	}
	defer os.RemoveAll(dir)
	download := func(key, name string, fetch func(context.Context, string) ([]byte, string, error)) (string, error) {
		u, _, e := f.storage.SignedURL(c, key, 30*time.Minute)
		if e != nil {
			return "", e
		}
		data, _, e := fetch(c, u)
		if e != nil {
			return "", e
		}
		path := filepath.Join(dir, name)
		return path, os.WriteFile(path, data, 0o600)
	}
	clipPath, e := download(*j.OutputMediaKey, "clip.mp4", f.voice.FetchVideo)
	if e != nil {
		return fail(e)
	}
	linePath, e := download(st.LineKey, "line.mp3", f.voice.FetchAudio)
	if e != nil {
		return fail(e)
	}
	length, e := videoprocessing.MediaDuration(c, clipPath)
	if e != nil || length <= 0 {
		return fail(fmt.Errorf("clip length: %v", e))
	}
	tracks := []videoprocessing.AudioTrack{}
	if st.At < length-0.05 {
		seconds := math.Min(st.Seconds, length-st.At)
		tracks = append(tracks, videoprocessing.AudioTrack{Path: linePath, TrimEnd: seconds, At: st.At, Volume: 1, FadeOut: math.Min(0.08, seconds/4)})
	}
	// The clip's own sound is muted (the model may still voice the line), so a quiet room tone
	// keeps the clip from dropping into digital silence around the line.
	tone := filepath.Join(dir, "room-tone.wav")
	if e = os.WriteFile(tone, filmRoomTone(length, filmRoomToneDb), 0o600); e == nil {
		tracks = append(tracks, videoprocessing.AudioTrack{Path: tone, TrimEnd: length, Volume: 1, FadeIn: 0.05, FadeOut: 0.05})
	}
	in := frozen.Request
	resolution := "720p"
	if modelark.QualityToResolution(in.Quality) == "1080p" && !in.Draft {
		resolution = "1080p"
	}
	muted := filmDubClipVolume
	data, e := f.voice.Processor.Merge(c, videoprocessing.MergeRequest{
		Clips: []videoprocessing.Clip{{Path: clipPath, Volume: &muted}}, OutputResolution: resolution, OutputAspectRatio: in.AspectRatio,
		AudioVolume: 1, AudioTracks: tracks,
	})
	if e != nil || len(data) == 0 {
		return fail(fmt.Errorf("mix: %v", e))
	}
	key := fmt.Sprintf("studio-pipelines/%s/%s/dubbed-%s.mp4", user, p.ID, j.ID)
	if e = f.storage.Upload(c, key, data, "video/mp4"); e != nil {
		return fail(e)
	}
	st.DubbedKey = key
	pipelineVoiceWrite(p, st)
	p.Stage = "save_video"
	return nil
}
