package facade_open_api

// Film dubbing: dialogue of characters with a bound Voice-menu voice is generated with seed-audio
// and mixed into the shot clip right after the video provider returns it, so the stored clip,
// the shot preview and the export all carry the character's own voice (one flow, no extra step).
// The video prompt asks the provider to animate those lines without voicing them.

import (
	"context"
	"encoding/binary"
	"encoding/json"
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
	// filmDubClipVolume mutes the clip's own audio under dubbed lines: the provider may still speak
	// the lines (it ignored "do not voice" and, before generate_audio=false was sent, made audio).
	filmDubClipVolume = 0.0
	// filmMixMuted marks mixes that mute the clip's own audio (from version 1 on); older mixes kept
	// it at 55 % and can double the speech.
	filmMixMuted = 1
	// filmMixVersion is the current mix: muted clip audio plus a room-tone bed (2), so a dubbed clip
	// never drops to digital silence. Older mixes are redone from the raw clip on export.
	filmMixVersion = 2
	// filmRoomToneDb is the room-tone level, in dBFS RMS: heard as air, far under the dialogue.
	filmRoomToneDb = -50.0
	// filmDubRunesPerSecond estimates Thai speech length for pricing before the audio exists.
	filmDubRunesPerSecond = 12.0
	// filmDubRetryWindow bounds retries of a failing dub (measured from video submission).
	filmDubRetryWindow = 30 * time.Minute
)

// filmVoiceService validates and speaks character voices (implemented by VoiceFacade).
type filmVoiceService interface {
	characterVoiceResolver
	SpeakLine(ctx context.Context, userID string, v models.CharacterVoice, text, delivery string) ([]byte, error)
	// SpeakLineAs speaks a line in the voice of anchor (a clip of the same character), so every
	// line of a character keeps one timbre; v still gives the emotion and speed.
	SpeakLineAs(ctx context.Context, userID string, v models.CharacterVoice, text, delivery string, anchor []byte) ([]byte, error)
}

// filmVoiceAnchorText is the neutral sample a character's anchor voice reads once. A described
// voice (preset or design) is sampled anew on every call, so without an anchor the same character
// changes speaker from shot to shot.
const filmVoiceAnchorText = "วันนี้อากาศดีนะ เดี๋ยวประชุมเสร็จแล้วเราค่อยคุยกันต่อ ไม่ต้องรีบหรอก ค่อยๆ คิดไปด้วยกัน"

// filmVoiceIdentity identifies who a voice is, without its mood: the anchor is shared by every line.
func filmVoiceIdentity(v models.CharacterVoice) string {
	return storyHash(struct {
		ID, Kind, Template string
		Speed, Pitch       float64
	}{v.VoiceID, v.Kind, v.TemplateID, v.Speed, float64(v.Pitch)})[:16]
}

// voiceAnchor returns the character's anchor clip, generating and storing it on first use.
func (f *FilmFacade) voiceAnchor(c context.Context, user, speakerID string, v models.CharacterVoice) ([]byte, error) {
	key := fmt.Sprintf("film/%s/voices/%s-%s.mp3", user, speakerID, filmVoiceIdentity(v))
	if f.storage != nil && f.options.MusicFetch != nil {
		if u, _, e := f.storage.SignedURL(c, key, 10*time.Minute); e == nil {
			if data, _, e := f.options.MusicFetch(c, u); e == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	neutral := v
	neutral.Emotion, neutral.TemplateID = "ปกติ", v.TemplateID
	data, e := f.voices.SpeakLine(c, user, neutral, filmVoiceAnchorText, "Natural, relaxed everyday conversation, as an actor speaks on a film set: unhurried, real breaths, not a narrator or announcer. No music.")
	if e != nil {
		return nil, e
	}
	if f.storage != nil {
		if e = f.storage.Upload(c, key, data, "audio/mpeg"); e != nil {
			log.Printf("film: voice anchor %s not stored: %v", key, e)
		}
	}
	return data, nil
}

// filmVoiceLine is one dubbed line frozen into a shot job.
type filmVoiceLine struct {
	SpeakerID string                `json:"speakerId"`
	Text      string                `json:"text"`
	Delivery  string                `json:"delivery,omitempty"`
	Voice     models.CharacterVoice `json:"voice"`
	// Scene is what happens in the shot, so the line is performed for its moment.
	Scene string `json:"scene,omitempty"`
}

// performance is the note the voice model gets (never read aloud): delivery and the shot's moment.
func (l filmVoiceLine) performance() string {
	parts := []string{}
	if d := strings.TrimSpace(l.Delivery); d != "" {
		parts = append(parts, d)
	}
	if s := strings.TrimSpace(l.Scene); s != "" {
		parts = append(parts, "Scene: "+s)
	}
	return strings.Join(parts, ". ")
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
			voice := *v
			if l.Emotion != "" {
				// The line's emotion (chosen for this shot) wins over the character's default.
				voice.Emotion, voice.TemplateID = l.Emotion, ""
			}
			out = append(out, filmVoiceLine{SpeakerID: l.SpeakerID, Text: l.Line, Delivery: l.Delivery, Voice: voice, Scene: directorTruncate(sh.Action, 220)})
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

// filmDubLine is one generated dialogue line kept next to a clip: its audio file, length and the
// time in the clip where it was placed. Voice is its index in the job's voice lines.
type filmDubLine struct {
	Voice     int     `json:"voice"`
	SpeakerID string  `json:"speakerId"`
	Text      string  `json:"text"`
	Key       string  `json:"key"`
	Seconds   float64 `json:"seconds"`
	At        float64 `json:"at"`
}

// filmDubbed is a generated line before it is stored.
type filmDubbed struct {
	filmDubLine
	Path string
	Data []byte
}

// filmDubMaxTempo is the fastest a line is played to fit its shot (faster sounds rushed).
const filmDubMaxTempo = 1.3

// speakLines generates the voice lines into dir in each character's anchor voice, gives them the
// production-sound treatment (pauses tightened, mic EQ, room, steady level) and places them one after
// another from filmDubLead. Lines that would still run past the shot (seconds) are played faster
// (up to filmDubMaxTempo, pitch kept) with a shorter lead and gaps, so the picture, its lip-sync and
// the voice end together. Lines whose voice no longer exists are skipped.
func (f *FilmFacade) speakLines(c context.Context, user, shotID string, voices []filmVoiceLine, dir string, seconds int) ([]filmDubbed, error) {
	out, raws, e := f.speakRaw(c, user, shotID, voices, dir, "", 1)
	if e != nil || len(out) == 0 {
		return out, e
	}
	return f.fitLines(c, user, shotID, voices, dir, seconds, out, raws)
}

// speakRaw generates the raw takes of the lines (suffix names the take, pace is added to the
// delivery note and speed multiplies the voice speed).
func (f *FilmFacade) speakRaw(c context.Context, user, shotID string, voices []filmVoiceLine, dir, pace string, speed float64) ([]filmDubbed, []string, error) {
	out := []filmDubbed{}
	raws := []string{}
	anchors := map[string][]byte{}
	tag := ""
	if pace != "" {
		tag = "-fast"
	}
	for i, l := range voices {
		var audio []byte
		voice := l.Voice
		if speed != 1 {
			base := voice.Speed
			if base <= 0 {
				base = 1
			}
			voice.Speed = math.Min(2, base*speed)
		}
		note := l.performance()
		if pace != "" {
			note = pace + " " + note
		}
		anchor, e := anchors[l.SpeakerID], error(nil)
		if anchor == nil {
			if anchor, e = f.voiceAnchor(c, user, l.SpeakerID, l.Voice); e == nil {
				anchors[l.SpeakerID] = anchor
			}
		}
		if e == nil {
			audio, e = f.voices.SpeakLineAs(c, user, voice, l.Text, note, anchor)
		}
		if errors.Is(e, ErrValidation) {
			log.Printf("film: shot=%s line %d skipped, voice %s no longer available", shotID, i, l.Voice.VoiceID)
			continue
		}
		if errors.Is(e, ErrVoiceRejected) || errors.Is(e, ErrVoiceNotGranted) || errors.Is(e, ErrVoiceUnavailable) {
			return nil, nil, e
		}
		if e != nil {
			return nil, nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
		}
		raw := filepath.Join(dir, fmt.Sprintf("line-%02d%s-raw.mp3", i, tag))
		if e = os.WriteFile(raw, audio, 0o600); e != nil {
			return nil, nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
		}
		raws = append(raws, raw)
		out = append(out, filmDubbed{filmDubLine: filmDubLine{Voice: i, SpeakerID: l.SpeakerID, Text: l.Text}, Path: filepath.Join(dir, fmt.Sprintf("line-%02d%s.mp3", i, tag))})
	}
	return out, raws, nil
}

// filmDubPace asks for a quicker take when the first one is too slow for its shot.
const filmDubPace = "Pace: brisk, normal conversational speed with no long pauses; the line is short and must be over quickly."

// fitLines treats and places the lines; if they run past the shot even played 1.3x faster, the
// lines are spoken once more at a brisk pace and the shorter result is kept.
func (f *FilmFacade) fitLines(c context.Context, user, shotID string, voices []filmVoiceLine, dir string, seconds int, out []filmDubbed, raws []string) ([]filmDubbed, error) {
	place := func(out []filmDubbed, raws []string, tempo, lead, gap float64) (float64, error) {
		at := lead
		for k := range out {
			if e := videoprocessing.TreatDialogueFile(c, raws[k], out[k].Path, tempo); e != nil {
				// Without ffmpeg filters the raw line is used as it is.
				log.Printf("film: shot=%s line %d dialogue treatment skipped: %v", shotID, out[k].Voice, e)
				if data, e := os.ReadFile(raws[k]); e == nil {
					_ = os.WriteFile(out[k].Path, data, 0o600)
				}
			}
			data, e := os.ReadFile(out[k].Path)
			if e != nil {
				return 0, fmt.Errorf("%w: %v", errFilmDubRetry, e)
			}
			d, e := videoprocessing.AudioDuration(c, out[k].Path)
			if e != nil {
				return 0, fmt.Errorf("%w: line %d: %v", errFilmDubRetry, k, e)
			}
			out[k].Data, out[k].Seconds, out[k].At = data, math.Round(d*1000)/1000, at
			at += d + gap
		}
		return at - gap, nil
	}
	end, e := place(out, raws, 1, filmDubLead, filmDubGap)
	if e != nil {
		return nil, e
	}
	budget := float64(seconds) - 0.3
	if seconds <= 0 || end <= budget {
		return out, nil
	}
	lead, gap := 0.15, 0.1
	squeeze := func(out []filmDubbed, raws []string) (float64, error) {
		speech := 0.0
		for _, l := range out {
			speech += l.Seconds
		}
		tempo := filmDubMaxTempo
		if room := budget - lead - gap*float64(len(out)-1); room > 0 {
			tempo = math.Max(1, math.Min(filmDubMaxTempo, speech/room*1.02))
		}
		log.Printf("film: shot=%s lines run %.1fs in a %ds shot; playing them at %.2fx", shotID, end, seconds, tempo)
		return place(out, raws, tempo, lead, gap)
	}
	if end, e = squeeze(out, raws); e != nil || end <= budget {
		return out, e
	}
	// Still too long: one quicker take, then the same squeeze; keep whichever ends sooner.
	fast, fastRaws, e := f.speakRaw(c, user, shotID, voices, dir, filmDubPace, 1.2)
	if e == nil && len(fast) == len(out) {
		if fastEnd, e := place(fast, fastRaws, 1, lead, gap); e == nil {
			if fastEnd > budget {
				fastEnd, e = squeeze(fast, fastRaws)
			}
			if e == nil && fastEnd < end {
				out, end = fast, fastEnd
			}
		}
	}
	if end > budget {
		log.Printf("film: shot=%s lines still run %.1fs; the tail is cut", shotID, end)
	}
	return out, nil
}

// mixLines mixes placed lines over a clip file. At is seconds from the clip start.
func (f *FilmFacade) mixLines(c context.Context, clipPath string, lines []filmDubbed, resolution, ratio string, draft bool) ([]byte, error) {
	tracks := make([]videoprocessing.AudioTrack, 0, len(lines))
	for _, l := range lines {
		tracks = append(tracks, videoprocessing.AudioTrack{Path: l.Path, TrimEnd: l.Seconds, At: l.At, Volume: 1, FadeOut: math.Min(0.08, l.Seconds/4)})
	}
	if dir := filepath.Dir(clipPath); len(lines) > 0 {
		// The clip's own sound is muted (the provider may speak the lines), so a quiet room tone
		// keeps the shot from falling into dead silence between and around the lines.
		if length, e := videoprocessing.MediaDuration(c, clipPath); e == nil && length > 0 {
			tone := filepath.Join(dir, "room-tone.wav")
			if e = os.WriteFile(tone, filmRoomTone(length, filmRoomToneDb), 0o600); e == nil {
				tracks = append(tracks, videoprocessing.AudioTrack{Path: tone, TrimEnd: length, Volume: 1, FadeIn: 0.05, FadeOut: 0.05})
			}
		}
	}
	out := "720p"
	if resolution == "1080p" && !draft {
		out = "1080p"
	}
	volume := filmDubClipVolume
	data, e := f.options.Processor.Merge(c, videoprocessing.MergeRequest{
		Clips: []videoprocessing.Clip{{Path: clipPath, Volume: &volume}}, OutputResolution: out, OutputAspectRatio: ratio,
		AudioVolume: 1, AudioTracks: tracks,
	})
	if e != nil || len(data) == 0 {
		return nil, fmt.Errorf("%w: mix: %v", errFilmDubRetry, e)
	}
	return data, nil
}

// dubClip generates the job's voice lines and mixes them over the clip. It returns the mixed clip
// and the lines (with their audio) so they can be stored and moved later without a new video.
func (f *FilmFacade) dubClip(c context.Context, user string, in *filmVideoJob, clip []byte) ([]byte, []filmDubbed, error) {
	if len(in.Voices) == 0 || f.voices == nil || f.options.Processor == nil {
		return clip, nil, nil
	}
	dir, e := os.MkdirTemp("", "film-dub-*")
	if e != nil {
		return nil, nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
	}
	defer os.RemoveAll(dir)
	clipPath := filepath.Join(dir, "clip.mp4")
	if e = os.WriteFile(clipPath, clip, 0o600); e != nil {
		return nil, nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
	}
	lines, e := f.speakLines(c, user, in.ShotID, in.Voices, dir, in.Seconds)
	if e != nil {
		return nil, nil, e
	}
	if len(lines) == 0 {
		// Never ship the provider's own voice for a dubbed shot.
		return nil, nil, fmt.Errorf("%w: shot %s: no line could be spoken", errFilmDubRetry, in.ShotID)
	}
	if end := lines[len(lines)-1].At + lines[len(lines)-1].Seconds; end > float64(in.Seconds)+0.05 {
		log.Printf("film: shot=%s dubbed lines run %.1fs, longer than the %ds clip; the tail is cut", in.ShotID, end, in.Seconds)
	}
	out, e := f.mixLines(c, clipPath, lines, in.Resolution, in.Ratio, in.Draft)
	if e != nil {
		return nil, nil, e
	}
	return out, lines, nil
}

// filmStoreLines uploads generated lines next to a clip (prefix without extension) and returns them.
func (f *FilmFacade) filmStoreLines(c context.Context, prefix string, lines []filmDubbed) ([]filmDubLine, error) {
	out := make([]filmDubLine, 0, len(lines))
	for _, l := range lines {
		l.Key = fmt.Sprintf("%s-line-%02d.mp3", prefix, l.Voice)
		if e := f.storage.Upload(c, l.Key, l.Data, "audio/mpeg"); e != nil {
			return nil, e
		}
		out = append(out, l.filmDubLine)
	}
	return out, nil
}

// prepareDialogue (voice first) generates the job's lines before the video, stores each line and
// the whole dialogue track (lines at their times on a silent track as long as the shot), and
// records them in the task so a retry never pays for the voices twice.
func (f *FilmFacade) prepareDialogue(c context.Context, j *models.FilmJob, in *filmVideoJob, task *filmVideoTask) error {
	if f.voices == nil || f.storage == nil {
		return fmt.Errorf("voice not configured")
	}
	dir, e := os.MkdirTemp("", "film-voice-first-*")
	if e != nil {
		return fmt.Errorf("%w: %v", errFilmDubRetry, e)
	}
	defer os.RemoveAll(dir)
	prefix := fmt.Sprintf("film/%s/%s/%s/%s", j.UserID, *j.ProjectID, in.ShotID, j.ID)
	lines := task.OwnedLines
	if len(lines) == 0 {
		spoken, e := f.speakLines(c, j.UserID.String(), in.ShotID, in.Voices, dir, in.Seconds)
		if e != nil {
			return e
		}
		if len(spoken) == 0 {
			return fmt.Errorf("%w: shot %s: no line could be spoken", errFilmDubRetry, in.ShotID)
		}
		if lines, e = f.filmStoreLines(c, prefix, spoken); e != nil {
			return fmt.Errorf("%w: store lines: %v", errFilmDubRetry, e)
		}
		task.OwnedLines = lines
		if e = f.recordTask(c, j, task); e != nil {
			return e
		}
	}
	placed, e := f.fetchLines(c, lines, dir)
	if e != nil {
		return e
	}
	tracks := make([]videoprocessing.AudioTrack, 0, len(placed))
	for _, l := range placed {
		if l.At >= float64(in.Seconds) {
			continue
		}
		tracks = append(tracks, videoprocessing.AudioTrack{Path: l.Path, TrimEnd: math.Min(l.Seconds, float64(in.Seconds)-l.At), At: l.At, Volume: 1})
	}
	if len(tracks) == 0 {
		return nil
	}
	track, e := videoprocessing.MixAudio(c, tracks, float64(in.Seconds))
	if e != nil {
		return fmt.Errorf("%w: dialogue track: %v", errFilmDubRetry, e)
	}
	key := prefix + "-dialogue.wav"
	if e = f.storage.Upload(c, key, track, "audio/wav"); e != nil {
		return fmt.Errorf("%w: store dialogue track: %v", errFilmDubRetry, e)
	}
	task.DialogueKey = key
	return f.recordTask(c, j, task)
}

// fetchLines downloads stored lines into dir for mixing.
func (f *FilmFacade) fetchLines(c context.Context, lines []filmDubLine, dir string) ([]filmDubbed, error) {
	if f.options.MusicFetch == nil {
		return nil, fmt.Errorf("audio fetch not configured")
	}
	out := make([]filmDubbed, 0, len(lines))
	for i, l := range lines {
		u, _, e := f.storage.SignedURL(c, l.Key, 30*time.Minute)
		var data []byte
		if e == nil {
			data, _, e = f.options.MusicFetch(c, u)
		}
		if e != nil {
			return nil, fmt.Errorf("%w: line %d: %v", errFilmDubRetry, i, e)
		}
		path := filepath.Join(dir, fmt.Sprintf("stored-%02d.mp3", i))
		if e = os.WriteFile(path, data, 0o600); e != nil {
			return nil, fmt.Errorf("%w: %v", errFilmDubRetry, e)
		}
		out = append(out, filmDubbed{filmDubLine: l, Path: path})
	}
	return out, nil
}

// mixStoredLines mixes already stored lines over a clip at their times.
func (f *FilmFacade) mixStoredLines(c context.Context, in *filmVideoJob, lines []filmDubLine, clip []byte) ([]byte, error) {
	if f.options.Processor == nil {
		return clip, nil
	}
	dir, e := os.MkdirTemp("", "film-mix-*")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	clipPath := filepath.Join(dir, "clip.mp4")
	if e = os.WriteFile(clipPath, clip, 0o600); e != nil {
		return nil, e
	}
	placed, e := f.fetchLines(c, lines, dir)
	if e != nil {
		return nil, e
	}
	length, e := videoprocessing.MediaDuration(c, clipPath)
	if e != nil {
		return nil, e
	}
	kept := placed[:0]
	for _, l := range placed {
		if l.At < length-0.05 {
			l.Seconds = math.Min(l.Seconds, length-l.At)
			kept = append(kept, l)
		}
	}
	if len(kept) == 0 {
		return clip, nil
	}
	return f.mixLines(c, clipPath, kept, in.Resolution, in.Ratio, in.Draft)
}

// recordTask stores the task state before the next paid or slow step.
func (f *FilmFacade) recordTask(c context.Context, j *models.FilmJob, task *filmVideoTask) error {
	raw, e := json.Marshal(task)
	if e != nil {
		return e
	}
	if e = f.store.RecordOutput(c, j, raw); e != nil {
		return e
	}
	j.Output = raw
	return nil
}

// filmRoomTone renders seconds of soft brown noise (16-bit mono WAV, 44.1 kHz) at about rmsDb dBFS:
// the low, even "air" of a quiet room. It is deterministic so a re-mix sounds the same.
func filmRoomTone(seconds, rmsDb float64) []byte {
	const rate = 44100
	n := int(seconds*rate) + 1
	samples := make([]float64, n)
	seed, level, sum := uint32(2463534242), 0.0, 0.0
	for i := range samples {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		white := float64(seed)/float64(math.MaxUint32)*2 - 1
		// Leaky integration turns white noise brown (weighted to low frequencies, like a room).
		level = 0.995*level + 0.02*white
		samples[i] = level
		sum += level * level
	}
	gain := 0.0
	if rms := math.Sqrt(sum / float64(n)); rms > 0 {
		gain = math.Pow(10, rmsDb/20) / rms
	}
	data := make([]byte, 44+2*n)
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(data[at:], v) }
	put16 := func(at int, v uint16) { binary.LittleEndian.PutUint16(data[at:], v) }
	copy(data, "RIFF")
	put32(4, uint32(36+2*n))
	copy(data[8:], "WAVEfmt ")
	put32(16, 16)
	put16(20, 1)
	put16(22, 1)
	put32(24, rate)
	put32(28, rate*2)
	put16(32, 2)
	put16(34, 16)
	copy(data[36:], "data")
	put32(40, uint32(2*n))
	for i, s := range samples {
		v := math.Max(-1, math.Min(1, s*gain))
		put16(44+2*i, uint16(int16(v*32767)))
	}
	return data
}
