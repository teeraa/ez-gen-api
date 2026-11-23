package facade_open_api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/seedaudio"
	"github.com/google/uuid"
)

var (
	// ErrVoiceUnavailable: BYTEPLUS_VOICE_API_KEY is not set.
	ErrVoiceUnavailable = errors.New("voice generation is not configured")
	// ErrVoiceRejected: the provider refused this input; the same request fails again.
	ErrVoiceRejected = errors.New("voice provider rejected the request")
	// ErrVoiceFailed: the provider failed; the same request may succeed later.
	ErrVoiceFailed = errors.New("voice generation failed")
	// ErrVoiceNotGranted: the BytePlus account or key has no access to seed-audio.
	ErrVoiceNotGranted = errors.New("voice provider access not granted")
	ErrVoiceStorage    = errors.New("voice storage unavailable")

	ErrVoiceProfileLimit = errors.New("voice profile limit reached")
	// ErrVoiceConsent: a reference clip was sent without the ownership/permission confirmation.
	ErrVoiceConsent              = errors.New("voice consent required")
	ErrVoiceReferenceInvalid     = errors.New("reference audio is not readable")
	ErrVoiceReferenceShort       = errors.New("reference audio is too short")
	ErrVoiceReferenceUnavailable = errors.New("reference audio processing unavailable")
)

const (
	// VoiceMaxTextRunes keeps Thai narration under seed-audio's 120-second output cap
	// (roughly 12-14 Thai characters per second at normal speed).
	VoiceMaxTextRunes = 1500
	voiceSignTTL      = time.Hour
	voiceListMax      = 50
	maxVoiceProfiles  = 20
	// voiceProducer is written into the implicit AIGC metadata of audio made from a user's clip.
	voiceProducer = "VIOLET STUDIO"
)

type voicePreset struct {
	ID, Name, Description, Color string
	// Prompt is sent to the model as the voice description; it is never read aloud.
	Prompt string
}

// The voice library is a set of natural-language voice descriptions, so no BytePlus speaker ID is needed.
var voicePresets = []voicePreset{
	{"nara", "นารา", "หญิง · อบอุ่น · เล่าเรื่อง", "#e06c9f", "Thai woman in her late twenties, warm and gentle storyteller, calm pacing, clear articulation"},
	{"krit", "กฤต", "ชาย · หนักแน่น · โฆษณา", "#3d7bd9", "Thai man in his thirties, deep and confident commercial announcer, punchy and persuasive"},
	{"ploy", "พลอย", "หญิง · สดใส · วัยรุ่น", "#f0a23b", "Thai teenage girl, bright, cheerful and energetic, friendly social-media style"},
	{"tawan", "ตะวัน", "ชาย · นุ่มลึก · สารคดี", "#3aa17e", "Thai man in his forties, soft and deep documentary narrator, thoughtful and measured"},
	{"mali", "มะลิ", "หญิง · สุภาพ · ข่าว", "#8b6cf0", "Thai woman news anchor, polite, formal and precise, neutral broadcast tone"},
	{"chai", "ชัย", "ชาย · ร่าเริง · การ์ตูน", "#d9553d", "Thai young man, playful and cheerful cartoon character voice, lively and expressive"},
}

// voiceEmotions is ordered as shown in the UI; "ปกติ" adds nothing to the prompt.
var voiceEmotions = []struct{ Label, Prompt string }{
	{"ปกติ", ""},
	{"ตื่นเต้น", "excited and energetic"},
	{"อบอุ่น", "warm and affectionate"},
	{"จริงจัง", "serious and firm"},
	{"เศร้า", "sad and subdued"},
	{"กระซิบ", "whispering softly"},
	{"ตะโกน", "shouting loudly with force, voice projected over noise"},
	{"โกรธ", "angry and tense, clipped words"},
	{"หวาดกลัว", "fearful, trembling and breathless"},
}

type voiceGenerator interface {
	Generate(context.Context, seedaudio.Request) (*seedaudio.Result, error)
}

type voiceStore interface {
	Upload(context.Context, string, []byte, string) error
	SignedURL(context.Context, string, time.Duration) (string, time.Time, error)
	SignedDownloadURL(context.Context, string, time.Duration, string) (string, time.Time, error)
}

type voiceRepository interface {
	FindByRequest(context.Context, uuid.UUID, uuid.UUID) (*models.VoiceGeneration, error)
	Create(context.Context, *models.VoiceGeneration) error
	List(context.Context, uuid.UUID, int, int) ([]models.VoiceGeneration, int64, error)
	Find(context.Context, uuid.UUID, uuid.UUID) (*models.VoiceGeneration, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	ListProfiles(context.Context, uuid.UUID) ([]models.VoiceProfile, error)
	CountProfiles(context.Context, uuid.UUID) (int64, error)
	FindProfile(context.Context, uuid.UUID, uuid.UUID) (*models.VoiceProfile, error)
	CreateProfile(context.Context, *models.VoiceProfile) error
	DeleteProfile(context.Context, uuid.UUID, uuid.UUID) error
}

// VoiceFacade backs the Voice menu: text-to-speech with a saved history and the user's own voices.
type VoiceFacade struct {
	repo    voiceRepository
	storage voiceStore
	gen     voiceGenerator
	// prepare converts an uploaded or recorded clip; replaced in tests.
	prepare   func(context.Context, []byte) ([]byte, float64, error)
	refsReady func() bool
	client    *http.Client
}

// NewVoiceFacade treats a nil client (no API key) as "voice off".
func NewVoiceFacade(r voiceRepository, s voiceStore, client *seedaudio.Client) *VoiceFacade {
	f := &VoiceFacade{repo: r, storage: s, prepare: seedaudio.PrepareReference, refsReady: seedaudio.ReferenceToolsAvailable,
		client: &http.Client{Timeout: time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	if client != nil {
		f.gen = client
	}
	return f
}

type VoiceInput struct {
	RequestID string  `json:"requestId"`
	Text      string  `json:"text"`
	VoiceID   string  `json:"voiceId"`
	Emotion   string  `json:"emotion"`
	Speed     float64 `json:"speed"`
	Pitch     int     `json:"pitch"`
}

func (f *VoiceFacade) available() bool { return f.gen != nil && f.storage != nil }

func (f *VoiceFacade) Capabilities() dto.VoiceCapabilitiesDTO {
	out := dto.VoiceCapabilitiesDTO{Available: f.available(), Model: seedaudio.Model, MaxChars: VoiceMaxTextRunes, Voices: []dto.VoicePresetDTO{}, Emotions: []string{},
		MaxProfiles: maxVoiceProfiles, ReferenceAvailable: f.available() && f.refsReady(),
		ReferenceMinSeconds: seedaudio.MinReferenceSeconds, ReferenceMaxSeconds: seedaudio.MaxReferenceSeconds, ReferenceMaxBytes: seedaudio.MaxReferenceBytes}
	for _, v := range voicePresets {
		out.Voices = append(out.Voices, dto.VoicePresetDTO{ID: v.ID, Name: v.Name, Description: v.Description, Color: v.Color})
	}
	for _, e := range voiceEmotions {
		out.Emotions = append(out.Emotions, e.Label)
	}
	return out
}

func findVoicePreset(id string) (voicePreset, bool) {
	for _, v := range voicePresets {
		if v.ID == id {
			return v, true
		}
	}
	return voicePreset{}, false
}

func voiceEmotionPrompt(label string) (string, bool) {
	for _, e := range voiceEmotions {
		if e.Label == label {
			return e.Prompt, true
		}
	}
	return "", false
}

// normalizeVoiceInput trims and defaults the input, returning false when it is out of range.
// VoiceID is resolved later: a preset id or the id of one of the user's own voices.
func normalizeVoiceInput(in VoiceInput) (VoiceInput, uuid.UUID, bool) {
	in.Text = strings.TrimSpace(in.Text)
	in.Emotion = strings.TrimSpace(in.Emotion)
	in.VoiceID = strings.TrimSpace(in.VoiceID)
	if in.Emotion == "" {
		in.Emotion = voiceEmotions[0].Label
	}
	if in.Speed == 0 {
		in.Speed = 1
	}
	in.Speed = math.Round(in.Speed*10) / 10
	rid, e := uuid.Parse(in.RequestID)
	_, emotion := voiceEmotionPrompt(in.Emotion)
	n := utf8.RuneCountInString(in.Text)
	ok := e == nil && emotion && in.VoiceID != "" && len(in.VoiceID) <= 40 && n >= 1 && n <= VoiceMaxTextRunes && utf8.ValidString(in.Text) &&
		in.Speed >= 0.5 && in.Speed <= 2 && in.Pitch >= -12 && in.Pitch <= 12
	return in, rid, ok
}

// voiceStudioPrompt keeps the description apart from the text so the description is not read aloud.
func voiceStudioPrompt(description, emotion, text string) string {
	desc := description
	if e, _ := voiceEmotionPrompt(emotion); e != "" {
		desc += "; mood: " + e
	}
	return fmt.Sprintf("Voice: %s.\nSpeak the following text exactly, word for word, in its own language. Do not read the voice description aloud.\n%s", desc, text)
}

// voiceReferencePrompt asks the model to speak in the voice of the uploaded clip (@Audio1).
func voiceReferencePrompt(emotion, text string) string {
	mood := ""
	if e, _ := voiceEmotionPrompt(emotion); e != "" {
		mood = ", in a " + e + " mood"
	}
	return fmt.Sprintf("@Audio1 reads the following text exactly, word for word, in its own language%s. Do not read these instructions aloud.\n%s", mood, text)
}

// voiceSpeechRate maps a 0.5x-2.0x multiplier to seed-audio's speech_rate (-50 = 0.5x, 100 = 2.0x).
func voiceSpeechRate(speed float64) int {
	return int(math.Round(math.Max(-50, math.Min(100, (speed-1)*100))))
}

// resolveVoice builds the provider request for a preset or one of the user's own voices.
func (f *VoiceFacade) resolveVoice(ctx context.Context, uid uuid.UUID, in VoiceInput) (seedaudio.Request, string, *uuid.UUID, error) {
	req := seedaudio.Request{Audio: seedaudio.AudioConfig{Format: "mp3", SpeechRate: voiceSpeechRate(in.Speed), PitchRate: in.Pitch, EnableSubtitle: true}}
	if p, ok := findVoicePreset(in.VoiceID); ok {
		req.TextPrompt = voiceStudioPrompt(p.Prompt, in.Emotion, in.Text)
		return req, p.Name, nil, nil
	}
	pid, e := uuid.Parse(in.VoiceID)
	if e != nil {
		return req, "", nil, ErrValidation
	}
	p, e := f.repo.FindProfile(ctx, pid, uid)
	if errors.Is(e, repo.ErrNotFound) {
		return req, "", nil, ErrValidation
	} else if e != nil {
		return req, "", nil, e
	}
	switch {
	case p.Kind == "design":
		req.TextPrompt = voiceStudioPrompt(p.Description, in.Emotion, in.Text)
	case p.Kind == "reference" && p.AudioKey != nil:
		clip, e := f.fetch(ctx, *p.AudioKey)
		if e != nil {
			return req, "", nil, e
		}
		req.TextPrompt = voiceReferencePrompt(in.Emotion, in.Text)
		req.References = []seedaudio.Reference{{AudioData: base64.StdEncoding.EncodeToString(clip)}}
		// Audio in a real person's voice is marked as AI-generated in its metadata.
		req.Watermark = &seedaudio.Watermark{AIGCMetadata: &seedaudio.AIGCMetadata{Enable: true, ContentProducer: voiceProducer, ProduceID: p.ID.String()}}
	default:
		return req, "", nil, ErrValidation
	}
	return req, p.Name, &p.ID, nil
}

// fetch reads a stored reference clip back from storage.
func (f *VoiceFacade) fetch(ctx context.Context, key string) ([]byte, error) {
	u, _, e := f.storage.SignedURL(ctx, key, 10*time.Minute)
	if e != nil {
		return nil, ErrVoiceStorage
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if e != nil {
		return nil, ErrVoiceStorage
	}
	res, e := f.client.Do(r)
	if e != nil {
		return nil, ErrVoiceStorage
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrVoiceStorage
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, seedaudio.MaxReferenceBytes+1))
	if e != nil || len(data) == 0 || len(data) > seedaudio.MaxReferenceBytes {
		return nil, ErrVoiceStorage
	}
	return data, nil
}

// Generate synthesizes speech and saves it to the history. created is false when the
// requestId was already used, in which case the first result is returned unchanged.
func (f *VoiceFacade) Generate(ctx context.Context, userID string, raw VoiceInput) (dto.VoiceGenerationDTO, bool, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return dto.VoiceGenerationDTO{}, false, ErrUnauthorized
	}
	in, rid, ok := normalizeVoiceInput(raw)
	if !ok {
		return dto.VoiceGenerationDTO{}, false, ErrValidation
	}
	if !f.available() {
		return dto.VoiceGenerationDTO{}, false, ErrVoiceUnavailable
	}
	if prior, e := f.repo.FindByRequest(ctx, uid, rid); e == nil {
		return f.replay(ctx, prior, in)
	} else if !errors.Is(e, repo.ErrNotFound) {
		return dto.VoiceGenerationDTO{}, false, e
	}

	req, voiceName, profileID, e := f.resolveVoice(ctx, uid, in)
	if e != nil {
		return dto.VoiceGenerationDTO{}, false, e
	}
	// Charged per requestId; refunded unless this call saves a new generation.
	pointRef := userID + ":" + rid.String()
	paid, e := chargePointsNew(ctx, userID, pointFeatureVoice, pointRef, pointPrices.Voice)
	if e != nil {
		return dto.VoiceGenerationDTO{}, false, e
	}
	saved := false
	defer func() {
		if paid && !saved {
			refundPoints(ctx, userID, pointFeatureVoice, pointRef)
		}
	}()
	out, e := f.gen.Generate(ctx, req)
	if e != nil {
		log.Printf("voice: user=%s generate failed: %v", uid, e)
		switch {
		case errors.Is(e, seedaudio.ErrNotGranted):
			return dto.VoiceGenerationDTO{}, false, ErrVoiceNotGranted
		case errors.Is(e, seedaudio.ErrRejected):
			return dto.VoiceGenerationDTO{}, false, ErrVoiceRejected
		}
		return dto.VoiceGenerationDTO{}, false, ErrVoiceFailed
	}
	if len(out.Audio) == 0 {
		return dto.VoiceGenerationDTO{}, false, ErrVoiceFailed
	}

	id := uuid.New()
	key := fmt.Sprintf("voice/%s/%s.mp3", uid, id)
	if e := f.storage.Upload(ctx, key, out.Audio, "audio/mpeg"); e != nil {
		log.Printf("voice: user=%s upload failed: %v", uid, e)
		return dto.VoiceGenerationDTO{}, false, ErrVoiceStorage
	}
	sentences := make([]dto.VoiceSentenceDTO, 0, len(out.Sentences))
	for _, s := range out.Sentences {
		sentences = append(sentences, dto.VoiceSentenceDTO{Start: float64(s.StartMS) / 1000, End: float64(s.EndMS) / 1000, Text: s.Text})
	}
	duration := out.Duration
	if duration <= 0 && len(sentences) > 0 {
		duration = sentences[len(sentences)-1].End
	}
	encoded, _ := json.Marshal(sentences)
	row := &models.VoiceGeneration{
		Base: models.Base{ID: id}, UserID: uid, RequestID: rid, Model: seedaudio.Model,
		Text: in.Text, VoiceID: in.VoiceID, VoiceName: voiceName, ProfileID: profileID, Emotion: in.Emotion, Speed: in.Speed, Pitch: in.Pitch,
		AudioKey: key, SizeBytes: int64(len(out.Audio)), Duration: duration, OriginalDuration: out.OriginalDuration, Sentences: encoded,
	}
	if e := f.repo.Create(ctx, row); e != nil {
		if errors.Is(e, repo.ErrConflict) {
			// A concurrent retry with the same requestId saved first; return that one.
			if prior, e2 := f.repo.FindByRequest(ctx, uid, rid); e2 == nil {
				return f.replay(ctx, prior, in)
			}
		}
		return dto.VoiceGenerationDTO{}, false, e
	}
	saved = true
	d, e := f.toDTO(ctx, row)
	return d, true, e
}

// replay returns a saved result for a reused requestId, refusing it when the body differs.
func (f *VoiceFacade) replay(ctx context.Context, prior *models.VoiceGeneration, in VoiceInput) (dto.VoiceGenerationDTO, bool, error) {
	if prior.Text != in.Text || prior.VoiceID != in.VoiceID || prior.Emotion != in.Emotion || prior.Speed != in.Speed || prior.Pitch != in.Pitch {
		return dto.VoiceGenerationDTO{}, false, ErrConflict
	}
	d, e := f.toDTO(ctx, prior)
	return d, false, e
}

func (f *VoiceFacade) List(ctx context.Context, userID string, page, limit int) (dto.VoiceGenerationListDTO, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return dto.VoiceGenerationListDTO{}, ErrUnauthorized
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > voiceListMax {
		limit = 20
	}
	rows, total, e := f.repo.List(ctx, uid, limit, (page-1)*limit)
	if e != nil {
		return dto.VoiceGenerationListDTO{}, e
	}
	out := dto.VoiceGenerationListDTO{Items: make([]dto.VoiceGenerationDTO, 0, len(rows)), Total: total, Page: page, Limit: limit}
	for i := range rows {
		d, e := f.toDTO(ctx, &rows[i])
		if e != nil {
			return dto.VoiceGenerationListDTO{}, e
		}
		out.Items = append(out.Items, d)
	}
	return out, nil
}

func (f *VoiceFacade) Delete(ctx context.Context, userID, id string) error {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return ErrUnauthorized
	}
	vid, e := uuid.Parse(id)
	if e != nil {
		return ErrNotFound
	}
	if e := f.repo.Delete(ctx, vid, uid); e != nil {
		if errors.Is(e, repo.ErrNotFound) {
			return ErrNotFound
		}
		return e
	}
	return nil
}

func voiceDisplayName(row *models.VoiceGeneration) string {
	if row.VoiceName != "" {
		return row.VoiceName
	}
	if p, ok := findVoicePreset(row.VoiceID); ok {
		return p.Name
	}
	return row.VoiceID
}

func voiceFileName(row *models.VoiceGeneration) string {
	return fmt.Sprintf("เสียงพากย์-%s-%s.mp3", voiceDisplayName(row), row.CreatedAt.In(marketplaceZone).Format("20060102-1504"))
}

func (f *VoiceFacade) toDTO(ctx context.Context, row *models.VoiceGeneration) (dto.VoiceGenerationDTO, error) {
	d := dto.VoiceGenerationDTO{ID: row.ID.String(), Text: row.Text, VoiceID: row.VoiceID, VoiceName: voiceDisplayName(row), Emotion: row.Emotion,
		Speed: row.Speed, Pitch: row.Pitch, Duration: row.Duration, SizeBytes: row.SizeBytes, Sentences: []dto.VoiceSentenceDTO{}, CreatedAt: row.CreatedAt}
	if row.ProfileID != nil {
		s := row.ProfileID.String()
		d.ProfileID = &s
	}
	if len(row.Sentences) > 0 {
		_ = json.Unmarshal(row.Sentences, &d.Sentences)
	}
	if f.storage == nil {
		return d, ErrVoiceStorage
	}
	u, exp, e := f.storage.SignedURL(ctx, row.AudioKey, voiceSignTTL)
	if e != nil {
		return d, ErrVoiceStorage
	}
	dl, _, e := f.storage.SignedDownloadURL(ctx, row.AudioKey, voiceSignTTL, voiceFileName(row))
	if e != nil {
		return d, ErrVoiceStorage
	}
	d.AudioURL, d.DownloadURL, d.ExpiresAt = u, dl, &exp
	return d, nil
}

// ── My voices ─────────────────────────────────────────────────────────────────

func voiceProfileName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= 40 && utf8.ValidString(name)
}

func (f *VoiceFacade) ListProfiles(ctx context.Context, userID string) ([]dto.VoiceProfileDTO, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return nil, ErrUnauthorized
	}
	rows, e := f.repo.ListProfiles(ctx, uid)
	if e != nil {
		return nil, e
	}
	out := make([]dto.VoiceProfileDTO, 0, len(rows))
	for i := range rows {
		d, e := f.profileDTO(ctx, &rows[i])
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, nil
}

// roomForProfile enforces the per-user cap before any upload work is done.
func (f *VoiceFacade) roomForProfile(ctx context.Context, uid uuid.UUID) error {
	n, e := f.repo.CountProfiles(ctx, uid)
	if e != nil {
		return e
	}
	if n >= maxVoiceProfiles {
		return ErrVoiceProfileLimit
	}
	return nil
}

// CreateDesignProfile saves a voice described in words, e.g. "ผู้ชายอายุ 50 เสียงแหบ พูดช้า".
func (f *VoiceFacade) CreateDesignProfile(ctx context.Context, userID, name, description string) (dto.VoiceProfileDTO, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return dto.VoiceProfileDTO{}, ErrUnauthorized
	}
	name, ok := voiceProfileName(name)
	description = strings.Join(strings.Fields(description), " ")
	if n := utf8.RuneCountInString(description); !ok || n < 5 || n > 300 || !utf8.ValidString(description) {
		return dto.VoiceProfileDTO{}, ErrValidation
	}
	if e := f.roomForProfile(ctx, uid); e != nil {
		return dto.VoiceProfileDTO{}, e
	}
	row := &models.VoiceProfile{Base: models.Base{ID: uuid.New()}, UserID: uid, Name: name, Kind: "design", Description: description}
	if e := f.repo.CreateProfile(ctx, row); e != nil {
		return dto.VoiceProfileDTO{}, e
	}
	return f.profileDTO(ctx, row)
}

// CreateReferenceProfile stores the user's own voice clip (uploaded file or browser recording).
// consent must be true: the user confirms the voice is theirs or used with the owner's permission.
func (f *VoiceFacade) CreateReferenceProfile(ctx context.Context, userID, name string, consent bool, data []byte) (dto.VoiceProfileDTO, error) {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return dto.VoiceProfileDTO{}, ErrUnauthorized
	}
	name, ok := voiceProfileName(name)
	if !ok || len(data) == 0 {
		return dto.VoiceProfileDTO{}, ErrValidation
	}
	if !consent {
		return dto.VoiceProfileDTO{}, ErrVoiceConsent
	}
	if f.storage == nil || !f.refsReady() {
		return dto.VoiceProfileDTO{}, ErrVoiceReferenceUnavailable
	}
	if e := f.roomForProfile(ctx, uid); e != nil {
		return dto.VoiceProfileDTO{}, e
	}
	clip, seconds, e := f.prepare(ctx, data)
	switch {
	case errors.Is(e, seedaudio.ErrReferenceShort):
		return dto.VoiceProfileDTO{}, ErrVoiceReferenceShort
	case errors.Is(e, seedaudio.ErrReferenceTool):
		return dto.VoiceProfileDTO{}, ErrVoiceReferenceUnavailable
	case e != nil:
		return dto.VoiceProfileDTO{}, ErrVoiceReferenceInvalid
	}
	id := uuid.New()
	key := fmt.Sprintf("voice-profiles/%s/%s.mp3", uid, id)
	if e := f.storage.Upload(ctx, key, clip, "audio/mpeg"); e != nil {
		log.Printf("voice: user=%s profile upload failed: %v", uid, e)
		return dto.VoiceProfileDTO{}, ErrVoiceStorage
	}
	now := time.Now()
	row := &models.VoiceProfile{Base: models.Base{ID: id}, UserID: uid, Name: name, Kind: "reference",
		AudioKey: &key, AudioSeconds: math.Round(seconds*10) / 10, AudioBytes: int64(len(clip)), ConsentAt: &now}
	if e := f.repo.CreateProfile(ctx, row); e != nil {
		return dto.VoiceProfileDTO{}, e
	}
	return f.profileDTO(ctx, row)
}

func (f *VoiceFacade) DeleteProfile(ctx context.Context, userID, id string) error {
	uid, e := uuid.Parse(userID)
	if e != nil {
		return ErrUnauthorized
	}
	pid, e := uuid.Parse(id)
	if e != nil {
		return ErrNotFound
	}
	if e := f.repo.DeleteProfile(ctx, pid, uid); e != nil {
		if errors.Is(e, repo.ErrNotFound) {
			return ErrNotFound
		}
		return e
	}
	return nil
}

func (f *VoiceFacade) profileDTO(ctx context.Context, p *models.VoiceProfile) (dto.VoiceProfileDTO, error) {
	d := dto.VoiceProfileDTO{ID: p.ID.String(), Name: p.Name, Kind: p.Kind, Description: p.Description, AudioSeconds: p.AudioSeconds, CreatedAt: p.CreatedAt}
	if p.AudioKey != nil {
		if f.storage == nil {
			return d, ErrVoiceStorage
		}
		u, exp, e := f.storage.SignedURL(ctx, *p.AudioKey, voiceSignTTL)
		if e != nil {
			return d, ErrVoiceStorage
		}
		d.SampleURL, d.ExpiresAt = u, &exp
	}
	return d, nil
}
