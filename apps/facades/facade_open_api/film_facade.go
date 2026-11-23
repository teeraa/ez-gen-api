package facade_open_api

// Film (ภาพยนตร์) — isolated from Director. Elements (characters, locations, props) belong to a
// film series; each shot video is generated straight from element reference images
// (reference-to-video) or, for "continue" shots, from the previous clip's last frame.
// Realistic/cinematic characters are referenced through a faceless costume image (synthetic
// first, realistic in the video), because the video provider rejects realistic face references.
// Director code is only called, never changed.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

const (
	filmNameMax       = 80
	filmSpecMax       = 1200
	filmActionMax     = 2000
	filmCameraMax     = 300
	filmAudioMax      = 600
	filmLineMax       = 300
	filmDeliveryMax   = 120
	filmMaxLines      = 6
	filmMaxShots      = 40
	filmMaxCharacters = 4
	filmMaxProps      = 3
	// Seedance accepts up to 9 reference images; 4 characters + 1 location + 3 props stay below it.
	filmMaxReferences = 9
	filmMaxSeconds    = 15
	filmMinSeconds    = 4
	filmTotalMax      = 600
	filmDefaultModel  = "seedance-2-5"
)

var (
	ErrFilmNotFound    = repo.ErrNotFound
	ErrFilmIdempotency = repo.ErrFilmIdempotency
	ErrFilmDuplicate   = repo.ErrFilmDuplicate
	ErrFilmAdmission   = repo.ErrConflict
)

type filmStore interface {
	Series(context.Context, string, string) (*models.DirectorSeries, error)
	Elements(context.Context, string, string) ([]models.FilmElement, error)
	Element(context.Context, string, string) (*models.FilmElement, error)
	Episode(context.Context, string, string) (*models.DirectorProject, *models.DirectorSeries, *models.FilmEpisode, error)
	CreateElement(context.Context, *models.FilmElement, string, string, func(*models.FilmElement, repo.FilmAssetLoader) error) (*models.FilmElement, error)
	ChangeElement(context.Context, string, string, string, string, func(*models.FilmElement, *models.DirectorSeries, repo.FilmAssetLoader) ([]models.FilmJob, error)) (*models.FilmElement, error)
	ChangeEpisode(context.Context, string, string, string, string, func(*models.FilmEpisode, *models.DirectorProject, *models.DirectorSeries, []models.FilmElement) ([]models.FilmJob, error)) (*models.FilmEpisode, error)
	Claim(context.Context) (*models.FilmJob, error)
	RecordOutput(context.Context, *models.FilmJob, []byte) error
	Wait(context.Context, *models.FilmJob, []byte, time.Time) error
	CompleteElement(context.Context, *models.FilmJob, string, string, bool, func(*models.FilmElement) error) error
	CompleteEpisode(context.Context, *models.FilmJob, string, bool, func(*models.FilmEpisode) error) error
	ProgressEpisode(context.Context, *models.FilmJob, func(*models.FilmEpisode) error) error
	CompletePlan(context.Context, *models.FilmJob, func(*models.FilmEpisode, []models.FilmElement, *models.DirectorSeries) ([]models.FilmElement, error)) error
	MarkElementProcessing(context.Context, *models.FilmJob, string) error
	Balance(context.Context, string) (int64, error)
}

type FilmOptions struct {
	Enabled      bool
	ImageModel   string
	ImagePoints  int
	VideoPoints  int // price of five seconds of video
	DraftPoints  int // price of five seconds of a 480p draft; 0 turns drafts off
	VideoEnabled bool
	// ReferenceCheck confirms a signed URL is downloadable before it is sent to a provider.
	ReferenceCheck func(context.Context, string) error
	// Processor extracts a clip's last frame when the provider does not return one.
	Processor videoprocessing.VideoProcessor
	// Analyzer plans stories and shots from the chat (Director's configured story planner).
	Analyzer      modelark.StoryFlowAnalyzer
	AnalysisModel string
	PlanPoints    int
	// VideoFetch downloads stored clips for export.
	VideoFetch func(context.Context, string) ([]byte, string, error)
	// VoiceFirst generates dubbed lines before the video and sends them as reference audio so lips
	// follow them; a provider rejection falls back to dubbing after the video.
	VoiceFirst bool
	// MusicFetch downloads the stored music track for export.
	MusicFetch func(context.Context, string) ([]byte, string, error)
}

type FilmFacade struct {
	store   filmStore
	storage mediaStorage
	images  modelark.ImageGenerator
	video   modelark.Client
	fetcher mediaFetcher
	options FilmOptions
	voices  filmVoiceService
}

// WithCharacterVoices enables binding Voice-menu voices to character elements and dubbing
// their dialogue into shot clips.
func (f *FilmFacade) WithCharacterVoices(r filmVoiceService) *FilmFacade {
	f.voices = r
	return f
}

// filmVoice resolves an element voice request outside the element transaction.
func (f *FilmFacade) filmVoice(c context.Context, user string, in *req.CharacterVoiceRequest) ([]byte, error) {
	if f.voices == nil {
		return nil, directorInvalid("voice")
	}
	v, e := f.voices.ResolveCharacterVoice(c, user, *in)
	if errors.Is(e, ErrValidation) {
		return nil, directorInvalid("voice")
	} else if e != nil {
		return nil, e
	}
	return models.EncodeCharacterVoice(v), nil
}

func NewFilmFacade(s filmStore, st mediaStorage, i modelark.ImageGenerator, v modelark.Client, fe mediaFetcher, o FilmOptions) *FilmFacade {
	return &FilmFacade{store: s, storage: st, images: i, video: v, fetcher: fe, options: o}
}

func (f *FilmFacade) videoAvailable() bool {
	return f.options.VideoEnabled && f.video != nil && f.fetcher != nil
}

// ---------- Episode document ----------

type filmLine struct {
	SpeakerID string `json:"speakerId"`
	Line      string `json:"line"`
	Delivery  string `json:"delivery,omitempty"`
	// Emotion is a Voice-menu emotion for this line; empty uses the speaker's voice setting.
	Emotion string `json:"emotion,omitempty"`
}

type filmVideo struct {
	Status         string  `json:"status"`
	Progress       int     `json:"progress"`
	JobID          string  `json:"jobId,omitempty"`
	Hash           string  `json:"hash,omitempty"`
	ModelID        string  `json:"modelId,omitempty"`
	Seconds        int     `json:"seconds,omitempty"`
	Mode           string  `json:"mode,omitempty"`
	ClipKey        string  `json:"clipKey,omitempty"`
	FrameKey       string  `json:"frameKey,omitempty"`
	PointsCharged  int     `json:"pointsCharged,omitempty"`
	PointsRefunded int     `json:"pointsRefunded,omitempty"`
	Draft          bool    `json:"draft,omitempty"`
	FailureCode    *string `json:"failureCode,omitempty"`
	FailureMessage *string `json:"failureMessage,omitempty"`
	CanRetry       bool    `json:"canRetry,omitempty"`

	// SourceFrameKey is the previous clip's last frame a "continue" clip started on; a new previous
	// clip makes this one stale because the cut no longer matches.
	SourceFrameKey string `json:"sourceFrameKey,omitempty"`

	// RawKey is the provider's clip before dubbing; ClipKey is RawKey plus the voices of VoiceHash.
	// Both are empty on clips made before dubbing was kept apart ("legacy").
	RawKey    string     `json:"rawKey,omitempty"`
	VoiceHash string     `json:"voiceHash,omitempty"`
	Redub     *filmRedub `json:"redub,omitempty"`
	// Silent clips were made without provider audio, so dubbed lines cannot double the provider's speech.
	Silent bool `json:"silent,omitempty"`
	// Lines are the dialogue audio dubbed into ClipKey, kept apart so the studio can play and move them.
	Lines []filmDubLine `json:"lines,omitempty"`
	// Mix is the filmMixVersion ClipKey was mixed with; older mixes are redone from RawKey on export.
	Mix int `json:"mix,omitempty"`
}

type filmShot struct {
	ID           string     `json:"id"`
	Seconds      int        `json:"seconds"`
	Transition   string     `json:"transition"`
	CharacterIDs []string   `json:"characterIds"`
	LocationID   string     `json:"locationId,omitempty"`
	PropIDs      []string   `json:"propIds"`
	Action       string     `json:"action"`
	Camera       string     `json:"camera"`
	Dialogue     []filmLine `json:"dialogue"`
	Audio        string     `json:"audio,omitempty"`
	// TrimIn/TrimOut are studio cuts at the clip's start/end; they never make the video stale.
	TrimIn  float64 `json:"trimIn,omitempty"`
	TrimOut float64 `json:"trimOut,omitempty"`
	// LineAt moves the video's dubbed lines (same order as Video.Lines), in seconds from the clip
	// start; studio-only like trims, so it never makes the video stale.
	LineAt []float64  `json:"lineAt,omitempty"`
	Video  *filmVideo `json:"video,omitempty"`
}

type filmDocument struct {
	Shots         []filmShot     `json:"shots"`
	Story         *filmStory     `json:"story,omitempty"`
	Messages      []filmMessage  `json:"messages,omitempty"`
	Plan          *filmPlanState `json:"plan,omitempty"`
	TargetSeconds int            `json:"targetSeconds,omitempty"`
	Export        *filmExport    `json:"export,omitempty"`
	Music         *filmMusic     `json:"music,omitempty"`
	// Drafts holds each shot's latest successful draft task, kept apart from the shot's video so a
	// failed finalize can be retried. Provider task ids stay server-side.
	Drafts map[string]filmDraft `json:"drafts,omitempty"`
}

type filmDraft struct {
	TaskID    string    `json:"taskId"`
	Provider  string    `json:"provider"`
	ModelID   string    `json:"modelId"`
	Hash      string    `json:"hash"`
	Seconds   int       `json:"seconds"`
	CreatedAt time.Time `json:"createdAt"`

	// SourceFrameKey carries a continuation draft's starting frame over to its full clip.
	SourceFrameKey string `json:"sourceFrameKey,omitempty"`
	// Silent drafts were made without provider audio; the full clip inherits that.
	Silent bool `json:"silent,omitempty"`
	// Lines are the draft's dubbed lines; its full clip keeps them, so the lips stay in sync.
	Lines []filmDubLine `json:"lines,omitempty"`
}

// filmSourceChanged reports whether a continuation clip started on a frame that is no longer the
// previous shot's last frame (the previous shot was regenerated, removed or reordered).
func filmSourceChanged(shots []filmShot, i int) bool {
	v := shots[i].Video
	if v == nil || v.Status != "succeeded" || v.SourceFrameKey == "" {
		return false
	}
	if i == 0 || shots[i].Transition != "continue" {
		return true
	}
	prev := shots[i-1].Video
	return prev == nil || prev.FrameKey != v.SourceFrameKey
}

// filmFinalizable reports whether a shot's draft can still be refined into the full clip.
func filmFinalizable(d *filmDocument, sh *filmShot, hash string) (filmDraft, bool) {
	draft, ok := d.Drafts[sh.ID]
	if !ok || draft.TaskID == "" || draft.Hash != hash || draft.Seconds != sh.Seconds || filmActive(sh.Video) {
		return draft, false
	}
	return draft, time.Now().Before(modelark.DraftExpiresAt(draft.CreatedAt))
}

func filmDecode(ep *models.FilmEpisode) (filmDocument, error) {
	d := filmDocument{Shots: []filmShot{}}
	if len(ep.Document) > 0 {
		if e := json.Unmarshal(ep.Document, &d); e != nil {
			return d, e
		}
	}
	if d.Shots == nil {
		d.Shots = []filmShot{}
	}
	return d, nil
}

func filmEncode(ep *models.FilmEpisode, d *filmDocument) error {
	b, e := json.Marshal(d)
	if e == nil {
		ep.Document = b
	}
	return e
}

func filmFindShot(d *filmDocument, id string) (int, *filmShot) {
	for i := range d.Shots {
		if d.Shots[i].ID == id {
			return i, &d.Shots[i]
		}
	}
	return -1, nil
}

func filmActive(v *filmVideo) bool {
	return v != nil && (v.Status == "queued" || v.Status == "processing")
}

func filmFilmed(style string) bool { return style == "realistic" || style == "cinematic" }

// filmShotHash covers everything that shapes a shot's video, including the referenced elements'
// text and images, so a clip becomes stale when any of them changes. Voices are not part of it:
// they are dubbed over the clip and can change without a new video (filmVoiceStatus).
func filmShotHash(sh *filmShot, elements map[string]*models.FilmElement, style string) string {
	type ref struct{ ID, Name, Spec, Image, Synthetic string }
	refs := []ref{}
	ids := append(append(append([]string{}, sh.CharacterIDs...), sh.LocationID), sh.PropIDs...)
	for _, id := range ids {
		if el, ok := elements[id]; ok {
			refs = append(refs, ref{el.ID.String(), el.Name, el.Spec, filmString(el.ImageKey), filmString(el.SyntheticKey)})
		}
	}
	return storyHash(struct {
		Seconds                    int
		Transition, Action, Camera string
		Audio, Style               string
		Dialogue                   []filmLine
		Refs                       []ref
	}{sh.Seconds, sh.Transition, sh.Action, sh.Camera, sh.Audio, style, sh.Dialogue, refs})
}

func filmString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func filmElementMap(items []models.FilmElement) map[string]*models.FilmElement {
	out := map[string]*models.FilmElement{}
	for i := range items {
		out[items[i].ID.String()] = &items[i]
	}
	return out
}

// filmPrepared reports whether an element has the reference image a shot video uses.
func filmPrepared(el *models.FilmElement, filmed bool) bool {
	if el.Kind == "character" && filmed {
		return el.SyntheticKey != nil && *el.SyntheticKey != ""
	}
	return el.ImageKey != nil && *el.ImageKey != ""
}

// filmReferenceKey is the stored image sent to the video provider for an element.
func filmReferenceKey(el *models.FilmElement, filmed bool) string {
	if el.Kind == "character" && filmed {
		return filmString(el.SyntheticKey)
	}
	return filmString(el.ImageKey)
}

func filmBound(s string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= min && n <= max
}

func filmUUID(id string) bool { _, e := uuid.Parse(id); return e == nil }

// ---------- Elements ----------

func (f *FilmFacade) ListElements(c context.Context, seriesID, user string) (dto.FilmElementList, error) {
	out := dto.FilmElementList{Items: []dto.FilmElement{}}
	if !filmUUID(seriesID) {
		return out, ErrFilmNotFound
	}
	s, e := f.store.Series(c, seriesID, user)
	if e != nil {
		return out, e
	}
	items, e := f.store.Elements(c, seriesID, user)
	if e != nil {
		return out, e
	}
	for i := range items {
		el, e := f.presentElement(c, &items[i], filmFilmed(s.Style))
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, el)
	}
	return out, nil
}

func (f *FilmFacade) presentElement(c context.Context, el *models.FilmElement, filmed bool) (dto.FilmElement, error) {
	out := dto.FilmElement{ID: el.ID.String(), SeriesID: el.SeriesID.String(), Kind: el.Kind, Name: el.Name, Spec: el.Spec, Revision: el.Revision, ImageStatus: el.ImageStatus, Prepared: filmPrepared(el, filmed), UpdatedAt: el.UpdatedAt.UTC()}
	if el.Kind == "character" {
		out.Voice = characterVoiceView(el.Voice)
	}
	out.FailureCode, out.FailureMessage, out.CanRetry = el.FailureCode, el.FailureMessage, el.ImageStatus == "failed"
	if key := filmString(el.ImageKey); key != "" && f.storage != nil {
		u, t, e := f.storage.SignedURL(c, key, time.Hour)
		if e != nil {
			return out, e
		}
		out.ImageURL, out.ImageExpiresAt = &u, &t
	}
	if key := filmString(el.SyntheticKey); key != "" && filmed && el.Kind == "character" && f.storage != nil {
		u, _, e := f.storage.SignedURL(c, key, time.Hour)
		if e != nil {
			return out, e
		}
		out.SyntheticURL = &u
	}
	return out, nil
}

var filmAssetKinds = map[string]string{"character": "character", "location": "location", "prop": "prop"}

func (f *FilmFacade) CreateElement(c context.Context, seriesID, user string, in req.FilmElementRequest) (dto.FilmElement, error) {
	if !filmUUID(seriesID) {
		return dto.FilmElement{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmElement{}, directorInvalid("idempotencyKey")
	}
	if in.Kind == nil || filmAssetKinds[*in.Kind] == "" {
		return dto.FilmElement{}, directorInvalid("kind")
	}
	if in.Name == nil || !filmBound(*in.Name, 1, filmNameMax) {
		return dto.FilmElement{}, directorInvalid("name")
	}
	spec := ""
	if in.Spec != nil {
		spec = strings.TrimSpace(*in.Spec)
	}
	if !filmBound(spec, 0, filmSpecMax) {
		return dto.FilmElement{}, directorInvalid("spec")
	}
	owner, e := uuid.Parse(user)
	if e != nil {
		return dto.FilmElement{}, ErrFilmNotFound
	}
	s, e := f.store.Series(c, seriesID, user)
	if e != nil {
		return dto.FilmElement{}, e
	}
	var voice []byte
	if in.Voice != nil {
		if *in.Kind != "character" {
			return dto.FilmElement{}, directorInvalid("voice")
		}
		if voice, e = f.filmVoice(c, user, in.Voice); e != nil {
			return dto.FilmElement{}, e
		}
	}
	now := time.Now().UTC()
	el := &models.FilmElement{Base: models.Base{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}, UserID: owner, SeriesID: s.ID, Kind: *in.Kind, Name: strings.TrimSpace(*in.Name), Spec: spec, ImageStatus: "idle", Revision: 1, Voice: voice}
	hash := storyHash(struct {
		Action, Series string
		Input          req.FilmElementRequest
	}{"film-element-create", seriesID, in})
	el, e = f.store.CreateElement(c, el, in.IdempotencyKey, hash, func(el *models.FilmElement, load repo.FilmAssetLoader) error {
		if in.AssetID == nil {
			return nil
		}
		if !filmUUID(*in.AssetID) {
			return directorInvalid("assetId")
		}
		a, e := load(*in.AssetID)
		if e != nil || a.AssetType != filmAssetKinds[el.Kind] || a.StorageKey == nil || *a.StorageKey == "" {
			return directorInvalid("assetId")
		}
		key := *a.StorageKey
		el.ImageKey, el.ImageStatus = &key, "succeeded"
		// A character imported from the library keeps its voice unless the request chose one.
		if in.Voice == nil && el.Kind == "character" {
			el.Voice = a.Voice
		}
		return nil
	})
	if e != nil {
		return dto.FilmElement{}, e
	}
	return f.presentElement(c, el, filmFilmed(s.Style))
}

func (f *FilmFacade) UpdateElement(c context.Context, id, user string, in req.FilmElementRequest) (dto.FilmElement, error) {
	if !filmUUID(id) {
		return dto.FilmElement{}, ErrFilmNotFound
	}
	if in.Revision == nil {
		return dto.FilmElement{}, directorInvalid("revision")
	}
	if in.Kind != nil || in.AssetID != nil {
		return dto.FilmElement{}, directorInvalid("request")
	}
	var voice []byte
	if in.Voice != nil {
		var e error
		if voice, e = f.filmVoice(c, user, in.Voice); e != nil {
			return dto.FilmElement{}, e
		}
	}
	filmed := false
	el, e := f.store.ChangeElement(c, id, user, "", "", func(el *models.FilmElement, s *models.DirectorSeries, _ repo.FilmAssetLoader) ([]models.FilmJob, error) {
		filmed = filmFilmed(s.Style)
		if *in.Revision != el.Revision {
			return nil, &DirectorError{Status: 409, Code: "REVISION_CONFLICT", Message: "มีการแก้ไขจากอีกแท็บ กรุณาโหลดข้อมูลล่าสุด", CurrentRevision: directorPointer(el.Revision)}
		}
		changed := false
		if in.Name != nil {
			if !filmBound(*in.Name, 1, filmNameMax) {
				return nil, directorInvalid("name")
			}
			el.Name, changed = strings.TrimSpace(*in.Name), true
		}
		if in.Spec != nil {
			if !filmBound(*in.Spec, 0, filmSpecMax) {
				return nil, directorInvalid("spec")
			}
			el.Spec, changed = strings.TrimSpace(*in.Spec), true
		}
		if in.Voice != nil {
			if el.Kind != "character" {
				return nil, directorInvalid("voice")
			}
			el.Voice, changed = voice, true
		}
		if changed {
			el.Revision++
		}
		return nil, nil
	})
	if e != nil {
		return dto.FilmElement{}, e
	}
	return f.presentElement(c, el, filmed)
}

func (f *FilmFacade) DeleteElement(c context.Context, id, user string) error {
	if !filmUUID(id) {
		return ErrFilmNotFound
	}
	_, e := f.store.ChangeElement(c, id, user, "", "", func(el *models.FilmElement, _ *models.DirectorSeries, _ repo.FilmAssetLoader) ([]models.FilmJob, error) {
		if el.ImageStatus == "processing" {
			return nil, &DirectorError{Status: 409, Code: "ELEMENT_BUSY", Message: "กำลังสร้างภาพขององค์ประกอบนี้ กรุณารอให้เสร็จก่อนลบ"}
		}
		el.DeletedAt.Time, el.DeletedAt.Valid = time.Now(), true
		return nil, nil
	})
	return e
}

// filmElementJob is the frozen input of an element image job.
type filmElementJob struct {
	ElementID  string `json:"elementId"`
	Mode       string `json:"mode"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Spec       string `json:"spec"`
	Style      string `json:"style"`
	Ratio      string `json:"ratio"`
	Filmed     bool   `json:"filmed"`
	SourceKey  string `json:"sourceKey,omitempty"`
	ImageModel string `json:"imageModel"`
}

func (f *FilmFacade) RequestElementImage(c context.Context, id, user string, in req.FilmElementImageRequest) (dto.FilmElement, error) {
	if !filmUUID(id) {
		return dto.FilmElement{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmElement{}, directorInvalid("idempotencyKey")
	}
	mode := in.Mode
	if mode == "" {
		mode = "generate"
	}
	if mode != "generate" && mode != "prepare" {
		return dto.FilmElement{}, directorInvalid("mode")
	}
	if !f.options.Enabled || f.images == nil || f.options.ImageModel == "" {
		return dto.FilmElement{}, &DirectorError{Status: 503, Code: "IMAGE_UNAVAILABLE", Message: "ระบบสร้างภาพยังไม่พร้อมใช้งาน"}
	}
	filmed := false
	hash := storyHash(struct {
		Action, Element string
		Input           req.FilmElementImageRequest
	}{"film-element-image", id, in})
	el, e := f.store.ChangeElement(c, id, user, in.IdempotencyKey, hash, func(el *models.FilmElement, s *models.DirectorSeries, _ repo.FilmAssetLoader) ([]models.FilmJob, error) {
		filmed = filmFilmed(s.Style)
		if el.ImageStatus == "queued" || el.ImageStatus == "processing" {
			return nil, &DirectorError{Status: 409, Code: "ELEMENT_BUSY", Message: "กำลังสร้างภาพขององค์ประกอบนี้อยู่"}
		}
		snap := filmElementJob{ElementID: el.ID.String(), Mode: mode, Kind: el.Kind, Name: el.Name, Spec: el.Spec, Style: s.Style, Ratio: s.AspectRatio, Filmed: filmed, ImageModel: f.options.ImageModel}
		calls := 1
		if mode == "prepare" {
			if !filmed || el.Kind != "character" || filmString(el.ImageKey) == "" {
				return nil, directorInvalid("mode")
			}
			snap.SourceKey = *el.ImageKey
		} else if filmed && el.Kind == "character" {
			calls = 2 // portrait, then the faceless costume reference
		}
		raw, e := json.Marshal(snap)
		if e != nil {
			return nil, e
		}
		el.ImageStatus, el.FailureCode, el.FailureMessage = "queued", nil, nil
		return []models.FilmJob{{ID: uuid.New(), SeriesID: s.ID, Kind: "element_image", TargetID: "element:" + el.ID.String(), Snapshot: raw, Cost: f.options.ImagePoints * calls}}, nil
	})
	if e != nil {
		return dto.FilmElement{}, e
	}
	return f.presentElement(c, el, filmed)
}

// ---------- Episodes ----------

func (f *FilmFacade) GetEpisode(c context.Context, projectID, user string) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	p, s, ep, e := f.store.Episode(c, projectID, user)
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	elements, e := f.store.Elements(c, s.ID.String(), user)
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	balance, e := f.store.Balance(c, user)
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.presentEpisode(c, p, s, ep, elements, balance)
}

// filmModelLabel names a video model for people, e.g. "seedance-2-0-fast" → "Seedance 2.0 Fast".
func filmModelLabel(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		return id
	}
	label := strings.ToUpper(parts[0][:1]) + parts[0][1:] + " " + parts[1] + "." + parts[2]
	for _, p := range parts[3:] {
		if p != "" {
			label += " " + strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return label
}

func filmVideoModels(drafts bool) []dto.FilmVideoModel {
	out := []dto.FilmVideoModel{}
	for _, m := range directorVideoModels {
		cfg, ok := modelark.ProviderModel(m.Provider)
		if !ok || !cfg.SupportsRefImage {
			continue
		}
		out = append(out, dto.FilmVideoModel{ID: m.ID, Label: filmModelLabel(m.ID), MinSeconds: max(cfg.MinDuration, filmMinSeconds), MaxSeconds: min(cfg.MaxDuration, filmMaxSeconds), Audio: cfg.SupportsAudio, Default: m.ID == filmDefaultModel, Draft: drafts && cfg.SupportsDraft()})
	}
	return out
}

func (f *FilmFacade) presentEpisode(c context.Context, p *models.DirectorProject, s *models.DirectorSeries, ep *models.FilmEpisode, elements []models.FilmElement, balance int64) (dto.FilmEpisode, error) {
	d, e := filmDecode(ep)
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	filmed := filmFilmed(s.Style)
	out := dto.FilmEpisode{ProjectID: p.ID.String(), SeriesID: s.ID.String(), EpisodeNumber: p.EpisodeNumber, EpisodeTitle: p.EpisodeTitle, Revision: ep.Revision, Style: s.Style, AspectRatio: s.AspectRatio, Resolution: s.Resolution, Filmed: filmed, Shots: []dto.FilmShot{}, Elements: []dto.FilmElement{}, VideoModels: filmVideoModels(f.options.DraftPoints > 0), ImagePoints: f.options.ImagePoints, VideoPointsPer5s: f.options.VideoPoints, DraftPointsPer5s: f.options.DraftPoints, Balance: balance, PlanPoints: f.options.PlanPoints, PlanAvailable: f.options.Analyzer != nil && f.options.AnalysisModel != "", TargetSeconds: d.TargetSeconds, Messages: []dto.FilmMessage{}}
	if d.Story != nil {
		out.Story = &dto.FilmStory{Title: d.Story.Title, Logline: d.Story.Logline, Synopsis: d.Story.Synopsis, Theme: d.Story.Theme}
	}
	for _, m := range d.Messages {
		out.Messages = append(out.Messages, dto.FilmMessage{Role: m.Role, Text: m.Text, ShotID: m.ShotID, At: m.At})
	}
	if d.Plan != nil {
		out.Plan = &dto.FilmPlan{Status: d.Plan.Status, ShotID: d.Plan.ShotID, FailureMessage: d.Plan.FailureMessage}
	}
	out.ExportAvailable = f.options.Processor != nil && f.options.VideoFetch != nil
	out.RedubAvailable = f.redubAvailable()
	if m := d.Music; m != nil {
		out.Music = &dto.FilmMusic{Name: m.Name, Seconds: m.Seconds, Volume: m.Volume, Start: m.Start}
		if f.storage != nil {
			u, t, e := f.storage.SignedURL(c, m.Key, time.Hour)
			if e != nil {
				return out, e
			}
			out.Music.URL, out.Music.ExpiresAt = &u, &t
		}
	}
	if x := d.Export; x != nil {
		ex := &dto.FilmExport{Status: x.Status, Progress: x.Progress, ClipCount: x.ClipCount, Skipped: x.Skipped, Subtitles: x.Subtitles, FailureMessage: x.FailureMessage, Stale: x.Status == "succeeded" && x.Source != filmExportSourceOf(&d)}
		if x.Key != "" && f.storage != nil {
			u, t, e := f.storage.SignedURL(c, x.Key, time.Hour)
			if e != nil {
				return out, e
			}
			ex.URL, ex.ExpiresAt = &u, &t
		}
		out.Export = ex
	}
	byID := filmElementMap(elements)
	for i := range elements {
		el, e := f.presentElement(c, &elements[i], filmed)
		if e != nil {
			return out, e
		}
		out.Elements = append(out.Elements, el)
	}
	start := 0
	for i := range d.Shots {
		sh := &d.Shots[i]
		lines := make([]dto.FilmLine, 0, len(sh.Dialogue))
		for _, l := range sh.Dialogue {
			lines = append(lines, dto.FilmLine{SpeakerID: l.SpeakerID, Line: l.Line, Delivery: l.Delivery, Emotion: l.Emotion})
		}
		item := dto.FilmShot{ID: sh.ID, Index: i, Seconds: sh.Seconds, Start: start, Transition: sh.Transition, CharacterIDs: filmNonNil(sh.CharacterIDs), LocationID: sh.LocationID, PropIDs: filmNonNil(sh.PropIDs), Action: sh.Action, Camera: sh.Camera, Dialogue: lines, Audio: sh.Audio, TrimIn: sh.TrimIn, TrimOut: sh.TrimOut, LineAt: sh.LineAt}
		start += sh.Seconds
		if v := sh.Video; v != nil {
			hash := filmShotHash(sh, byID, s.Style)
			fresh := filmVideoFresh(v, sh, byID, s.Style)
			// A clip whose dubbed lines play over the provider's own speech needs a new (silent) video.
			overlap := v.Status == "succeeded" && filmVoiceOverlap(v, sh, byID, s.Style)
			video := &dto.FilmShotVideo{Status: v.Status, Progress: v.Progress, ModelID: v.ModelID, Seconds: v.Seconds, Mode: v.Mode, PointsCharged: v.PointsCharged, PointsRefunded: v.PointsRefunded, Stale: v.Status == "succeeded" && (!fresh || overlap || filmSourceChanged(d.Shots, i)), Draft: v.Draft}
			video.Voice = filmVoiceStatus(v, sh, byID, s.Style)
			if video.Voice == "failed" && v.Redub != nil {
				video.VoiceMessage = v.Redub.FailureMessage
			}
			if fresh && (video.Voice == "outdated" || video.Voice == "failed") {
				out.RedubShots++
				out.RedubPoints += filmVoicePoints(filmVoiceLines(sh, byID))
			}
			draft, finalizable := filmFinalizable(&d, sh, hash)
			video.CanFinalize = finalizable && filmDraftDubbable(draft, sh, byID)
			video.CanFinalize = video.CanFinalize && f.options.DraftPoints > 0
			video.FailureCode, video.FailureMessage, video.CanRetry = v.FailureCode, v.FailureMessage, v.CanRetry
			if v.ClipKey != "" && f.storage != nil {
				u, t, e := f.storage.SignedURL(c, v.ClipKey, time.Hour)
				if e != nil {
					return out, e
				}
				video.URL, video.ExpiresAt = &u, &t
			}
			video.Lines = []dto.FilmDubLine{}
			if len(v.Lines) > 0 && v.RawKey != "" && f.storage != nil {
				u, _, e := f.storage.SignedURL(c, v.RawKey, time.Hour)
				if e != nil {
					return out, e
				}
				video.RawURL = &u
				for _, l := range v.Lines {
					lu, _, e := f.storage.SignedURL(c, l.Key, time.Hour)
					if e != nil {
						return out, e
					}
					video.Lines = append(video.Lines, dto.FilmDubLine{SpeakerID: l.SpeakerID, Text: l.Text, URL: lu, Seconds: l.Seconds, At: l.At})
				}
			}
			item.Video = video
		}
		out.Shots = append(out.Shots, item)
	}
	out.TotalSeconds = start
	return out, nil
}

func filmNonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// filmValidateShots turns a full shot list into the stored form, keeping the video state of
// shots that keep their id. Shots with a running video may not change or disappear.
func filmValidateShots(in []req.FilmShotRequest, current []filmShot, elements map[string]*models.FilmElement, style string) ([]filmShot, error) {
	if len(in) > filmMaxShots {
		return nil, directorInvalid("shots")
	}
	old := map[string]*filmShot{}
	for i := range current {
		old[current[i].ID] = &current[i]
	}
	kind := func(id, want string) bool { el, ok := elements[id]; return ok && el.Kind == want }
	seen, out, total := map[string]bool{}, make([]filmShot, 0, len(in)), 0
	for i, r := range in {
		sh := filmShot{ID: r.ID, Seconds: r.Seconds, Transition: r.Transition, CharacterIDs: filmNonNil(r.CharacterIDs), LocationID: strings.TrimSpace(r.LocationID), PropIDs: filmNonNil(r.PropIDs), Action: strings.TrimSpace(r.Action), Camera: strings.TrimSpace(r.Camera), Audio: strings.TrimSpace(r.Audio), Dialogue: []filmLine{}}
		if sh.ID == "" {
			sh.ID = uuid.NewString()
		} else if old[sh.ID] == nil || seen[sh.ID] {
			return nil, directorInvalid("shots.id")
		}
		seen[sh.ID] = true
		if sh.Seconds < filmMinSeconds || sh.Seconds > filmMaxSeconds {
			return nil, directorInvalid("shots.seconds")
		}
		var ok bool
		if sh.TrimIn, sh.TrimOut, ok = filmTrim(r.TrimIn, r.TrimOut, sh.Seconds); !ok {
			return nil, directorInvalid("shots.trim")
		}
		if sh.LineAt, ok = filmLineAt(r.LineAt, sh.Seconds); !ok {
			return nil, directorInvalid("shots.lineAt")
		}
		total += sh.Seconds
		if sh.Transition == "" {
			sh.Transition = "cut"
		}
		if (sh.Transition != "cut" && sh.Transition != "continue") || (i == 0 && sh.Transition == "continue") {
			return nil, directorInvalid("shots.transition")
		}
		if len(sh.CharacterIDs) > filmMaxCharacters || len(sh.PropIDs) > filmMaxProps {
			return nil, directorInvalid("shots.references")
		}
		dup := map[string]bool{}
		for _, id := range sh.CharacterIDs {
			if !kind(id, "character") || dup[id] {
				return nil, directorInvalid("shots.characterIds")
			}
			dup[id] = true
		}
		for _, id := range sh.PropIDs {
			if !kind(id, "prop") || dup[id] {
				return nil, directorInvalid("shots.propIds")
			}
			dup[id] = true
		}
		if sh.LocationID != "" && !kind(sh.LocationID, "location") {
			return nil, directorInvalid("shots.locationId")
		}
		if !filmBound(sh.Action, 1, filmActionMax) {
			return nil, directorInvalid("shots.action")
		}
		if !filmBound(sh.Camera, 0, filmCameraMax) || !filmBound(sh.Audio, 0, filmAudioMax) {
			return nil, directorInvalid("shots.camera")
		}
		if len(r.Dialogue) > filmMaxLines {
			return nil, directorInvalid("shots.dialogue")
		}
		for _, l := range r.Dialogue {
			line := filmLine{SpeakerID: l.SpeakerID, Line: strings.TrimSpace(l.Line), Delivery: strings.TrimSpace(l.Delivery), Emotion: strings.TrimSpace(l.Emotion)}
			if _, ok := voiceEmotionPrompt(line.Emotion); line.Emotion != "" && !ok {
				return nil, directorInvalid("shots.dialogue.emotion")
			}
			if !dup[line.SpeakerID] || !kind(line.SpeakerID, "character") || !filmBound(line.Line, 1, filmLineMax) || !filmBound(line.Delivery, 0, filmDeliveryMax) {
				return nil, directorInvalid("shots.dialogue")
			}
			sh.Dialogue = append(sh.Dialogue, line)
		}
		if prev := old[sh.ID]; prev != nil {
			sh.Video = prev.Video
			if filmActive(prev.Video) && filmShotHash(prev, elements, style) != filmShotHash(&sh, elements, style) {
				return nil, &DirectorError{Status: 409, Code: "SHOT_BUSY", Message: "ช็อตนี้กำลังสร้างวิดีโอ กรุณารอให้เสร็จก่อนแก้"}
			}
		}
		out = append(out, sh)
	}
	if total > filmTotalMax {
		return nil, directorInvalid("shots.seconds")
	}
	for id, prev := range old {
		if !seen[id] && filmActive(prev.Video) {
			return nil, &DirectorError{Status: 409, Code: "SHOT_BUSY", Message: "ช็อตที่กำลังสร้างวิดีโอลบไม่ได้ กรุณารอให้เสร็จก่อน"}
		}
	}
	return out, nil
}

func (f *FilmFacade) SaveShots(c context.Context, projectID, user string, in req.FilmShotsRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if in.IdempotencyKey != "" && !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	hash := ""
	if in.IdempotencyKey != "" {
		hash = storyHash(struct {
			Action, Project string
			Input           req.FilmShotsRequest
		}{"film-shots", projectID, in})
	}
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		if in.Revision != ep.Revision {
			return nil, &DirectorError{Status: 409, Code: "REVISION_CONFLICT", Message: "มีการแก้ไขจากอีกแท็บ กรุณาโหลดข้อมูลล่าสุด", CurrentRevision: directorPointer(ep.Revision)}
		}
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		shots, e := filmValidateShots(in.Shots, d.Shots, filmElementMap(elements), s.Style)
		if e != nil {
			return nil, e
		}
		d.Shots = shots
		return nil, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}

// filmRef is one reference image of a shot, in the order the prompt labels them (Image 1, 2, ...).
type filmRef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// filmVideoJob is the frozen input of a shot video job.
type filmVideoJob struct {
	ShotID          string    `json:"shotId"`
	Hash            string    `json:"hash"`
	ModelID         string    `json:"modelId"`
	ProviderModel   string    `json:"providerModel"`
	Ratio           string    `json:"ratio"`
	Resolution      string    `json:"resolution"`
	Seconds         int       `json:"seconds"`
	Filmed          bool      `json:"filmed"`
	Transition      string    `json:"transition"`
	PrevShotID      string    `json:"prevShotId,omitempty"`
	ReferencePrompt string    `json:"referencePrompt"`
	ContinuePrompt  string    `json:"continuePrompt,omitempty"`
	Refs            []filmRef `json:"refs"`
	Audio           bool      `json:"audio"`
	Draft           bool      `json:"draft,omitempty"`
	// DraftTaskID refines that draft into the full clip; the provider reuses its prompt and frames.
	DraftTaskID string    `json:"draftTaskId,omitempty"`
	QueuedAt    time.Time `json:"queuedAt"`
	// Voices are dialogue lines dubbed with the speakers' bound voices after the clip returns.
	Voices []filmVoiceLine `json:"voices,omitempty"`
	// VoiceFirst sends the generated lines as reference audio (audio 1) so the lips follow them;
	// Plain* are the prompts used when the provider rejects the audio reference.
	VoiceFirst           bool   `json:"voiceFirst,omitempty"`
	PlainReferencePrompt string `json:"plainReferencePrompt,omitempty"`
	PlainContinuePrompt  string `json:"plainContinuePrompt,omitempty"`
	// Lines are already generated lines to reuse (a finalize keeps its draft's lines and timing).
	Lines []filmDubLine `json:"lines,omitempty"`
}

func filmVideoCost(points, seconds int) int {
	return (points*seconds + 4) / 5
}

func (f *FilmFacade) RequestVideo(c context.Context, projectID, shotID, user string, in req.FilmVideoRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if !f.options.Enabled || !f.videoAvailable() {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "VIDEO_UNAVAILABLE", Message: "ระบบสร้างวิดีโอยังไม่พร้อมใช้งาน"}
	}
	modelID := in.ModelID
	if modelID == "" {
		modelID = filmDefaultModel
	}
	model, ok := directorVideoModel(modelID)
	if !ok || !model.SupportsRefImage {
		return dto.FilmEpisode{}, directorInvalid("modelId")
	}
	if in.Draft && in.Finalize {
		return dto.FilmEpisode{}, directorInvalid("draft")
	}
	if (in.Draft || in.Finalize) && (!model.SupportsDraft() || f.options.DraftPoints <= 0) {
		return dto.FilmEpisode{}, &DirectorError{Status: 422, Code: "DRAFT_NOT_SUPPORTED", Message: "โมเดลนี้ไม่รองรับโหมด draft"}
	}
	hash := storyHash(struct {
		Action, Project, Shot string
		Input                 req.FilmVideoRequest
	}{"film-video", projectID, shotID, in})
	_, e := f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, s *models.DirectorSeries, elements []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		i, sh := filmFindShot(&d, shotID)
		if sh == nil {
			return nil, ErrFilmNotFound
		}
		if filmActive(sh.Video) {
			return nil, &DirectorError{Status: 409, Code: "SHOT_BUSY", Message: "ช็อตนี้กำลังสร้างวิดีโออยู่"}
		}
		if sh.Seconds < model.MinDuration || sh.Seconds > model.MaxDuration {
			return nil, directorInvalid("seconds")
		}
		filmed := filmFilmed(s.Style)
		byID := filmElementMap(elements)
		if in.Finalize {
			// The provider inherits prompt, frames, ratio and duration from the draft, so element and
			// dependency checks already passed when the draft was made.
			draft, ok := filmFinalizable(&d, sh, filmShotHash(sh, byID, s.Style))
			if !ok || draft.Provider != model.ProviderModelID || !filmDraftDubbable(draft, sh, byID) {
				return nil, &DirectorError{Status: 409, Code: "DRAFT_NOT_AVAILABLE", Message: "ไม่มี draft ล่าสุดของช็อตนี้ให้ทำเป็นตัวเต็ม กรุณาสร้าง draft ใหม่"}
			}
			snap := filmVideoJob{ShotID: sh.ID, Hash: draft.Hash, ModelID: draft.ModelID, ProviderModel: draft.Provider, Ratio: s.AspectRatio, Resolution: directorVideoResolution(s.Resolution, model), Seconds: sh.Seconds, Filmed: filmed, Transition: sh.Transition, DraftTaskID: draft.TaskID, Audio: !draft.Silent, QueuedAt: time.Now().UTC(), Voices: f.filmDubLines(sh, byID), Lines: draft.Lines}
			raw, e := json.Marshal(snap)
			if e != nil {
				return nil, e
			}
			jobID := uuid.New()
			cost := filmVideoCost(f.options.VideoPoints, sh.Seconds) + filmVoicePoints(snap.Voices)
			sh.Video = &filmVideo{Status: "queued", JobID: jobID.String(), Hash: snap.Hash, ModelID: snap.ModelID, Seconds: sh.Seconds, Mode: "finalize", PointsCharged: cost, SourceFrameKey: draft.SourceFrameKey}
			if e = filmEncode(ep, &d); e != nil {
				return nil, e
			}
			project := ep.ProjectID
			return []models.FilmJob{{ID: jobID, SeriesID: s.ID, ProjectID: &project, Kind: "shot_video", TargetID: "shot:" + sh.ID, Snapshot: raw, Cost: cost}}, nil
		}
		cast, location, props := []*models.FilmElement{}, (*models.FilmElement)(nil), []*models.FilmElement{}
		for _, id := range sh.CharacterIDs {
			cast = append(cast, byID[id])
		}
		if sh.LocationID != "" {
			location = byID[sh.LocationID]
		}
		for _, id := range sh.PropIDs {
			props = append(props, byID[id])
		}
		all := append(append(append([]*models.FilmElement{}, cast...), location), props...)
		for _, el := range all {
			if el == nil {
				continue
			}
			if !filmPrepared(el, filmed) {
				msg := "องค์ประกอบ “" + el.Name + "” ยังไม่มีภาพอ้างอิง กรุณาสร้างภาพก่อน"
				if filmed && el.Kind == "character" && filmString(el.ImageKey) != "" {
					msg = "ตัวละคร “" + el.Name + "” ยังไม่ได้เตรียมภาพสำหรับโหมดสมจริง กรุณากดเตรียมภาพก่อน"
				}
				return nil, &DirectorError{Status: 409, Code: "ELEMENT_NOT_READY", Message: msg}
			}
		}
		for _, el := range cast {
			if el == nil {
				return nil, &DirectorError{Status: 409, Code: "ELEMENT_NOT_READY", Message: "มีตัวละครในช็อตนี้ที่ถูกลบไปแล้ว กรุณาแก้ช็อตก่อน"}
			}
		}
		for _, el := range props {
			if el == nil {
				return nil, &DirectorError{Status: 409, Code: "ELEMENT_NOT_READY", Message: "มีของในช็อตนี้ที่ถูกลบไปแล้ว กรุณาแก้ช็อตก่อน"}
			}
		}
		// Every character keeps one voice across the film, so a shot waits until its whole cast has one.
		for _, el := range cast {
			if filmBoundVoice(el) == nil {
				return nil, filmVoiceRequired(el.Name)
			}
		}
		if sh.LocationID != "" && location == nil {
			return nil, &DirectorError{Status: 409, Code: "ELEMENT_NOT_READY", Message: "สถานที่ของช็อตนี้ถูกลบไปแล้ว กรุณาแก้ช็อตก่อน"}
		}
		brief := filmBrief{Shot: sh, Cast: cast, Location: location, Props: props, Style: s.Style, Filmed: filmed}
		dub := f.filmDubLines(sh, byID)
		for _, l := range dub {
			if brief.Dubbed == nil {
				brief.Dubbed = map[string]bool{}
			}
			brief.Dubbed[l.SpeakerID] = true
		}
		if sh.Transition == "continue" && i > 0 {
			listed := map[string]bool{}
			for _, id := range sh.CharacterIDs {
				listed[id] = true
			}
			for _, id := range d.Shots[i-1].CharacterIDs {
				if el := byID[id]; el != nil && !listed[id] {
					brief.InFrame = append(brief.InFrame, el)
				}
			}
		}
		refs := filmReferences(&brief)
		if len(refs) > filmMaxReferences {
			return nil, directorInvalid("shots.references")
		}
		// Shots with dubbed lines are made without provider audio: the provider voices dialogue even
		// when told not to, and its speech would play over the dubbed voices.
		snap := filmVideoJob{ShotID: sh.ID, Hash: filmShotHash(sh, byID, s.Style), ModelID: directorCanonicalVideoModel(modelID), ProviderModel: model.ProviderModelID, Ratio: s.AspectRatio, Resolution: directorVideoResolution(s.Resolution, model), Seconds: sh.Seconds, Filmed: filmed, Transition: sh.Transition, ReferencePrompt: filmReferencePrompt(&brief), Refs: refs, Audio: model.SupportsAudio && (len(sh.Dialogue) > 0 || sh.Audio != "") && len(dub) == 0, Draft: in.Draft, QueuedAt: time.Now().UTC(), Voices: dub}
		if sh.Transition == "continue" && i > 0 {
			snap.PrevShotID = d.Shots[i-1].ID
			snap.ContinuePrompt = filmContinuePrompt(&brief)
		}
		if f.options.VoiceFirst && len(dub) > 0 {
			// Voice first: the lines are generated before the video and sent as audio 1.
			snap.PlainReferencePrompt, snap.PlainContinuePrompt = snap.ReferencePrompt, snap.ContinuePrompt
			brief.AudioRef = true
			snap.VoiceFirst, snap.ReferencePrompt = true, filmReferencePrompt(&brief)
			if snap.ContinuePrompt != "" {
				snap.ContinuePrompt = filmContinuePrompt(&brief)
			}
		}
		for _, p := range []string{snap.ReferencePrompt, snap.ContinuePrompt} {
			if p != "" && filmValidatePrompt(p) != nil {
				return nil, &DirectorError{Status: 400, Code: "VIDEO_PROMPT_TOO_LONG", Message: "รายละเอียดช็อตรวมยาวเกินขีดจำกัด กรุณาย่อข้อความ"}
			}
		}
		raw, e := json.Marshal(snap)
		if e != nil {
			return nil, e
		}
		jobID := uuid.New()
		cost := filmVideoCost(f.options.VideoPoints, sh.Seconds)
		if in.Draft {
			cost = filmVideoCost(f.options.DraftPoints, sh.Seconds)
		}
		cost += filmVoicePoints(snap.Voices)
		mode := "reference"
		if sh.Transition == "continue" {
			mode = "continue"
		}
		sh.Video = &filmVideo{Status: "queued", JobID: jobID.String(), Hash: snap.Hash, ModelID: snap.ModelID, Seconds: sh.Seconds, Mode: mode, PointsCharged: cost, Draft: in.Draft}
		if e = filmEncode(ep, &d); e != nil {
			return nil, e
		}
		project := ep.ProjectID
		return []models.FilmJob{{ID: jobID, SeriesID: s.ID, ProjectID: &project, Kind: "shot_video", TargetID: "shot:" + sh.ID, Snapshot: raw, Cost: cost}}, nil
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}
