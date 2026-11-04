package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoeditor"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrVEInvalid = errors.New("video editor validation")
	ErrVEMedia   = errors.New("video editor media unavailable")
	ErrVEStorage = errors.New("video editor storage unavailable")
	ErrVEType    = errors.New("video editor invalid file")
	ErrVESize    = errors.New("video editor file too large")
	// ErrVETextUnavailable is a MEDIA_UNAVAILABLE case: drawtext or a Thai font is missing.
	ErrVETextUnavailable = fmt.Errorf("video editor text overlays unavailable: %w", ErrVEMedia)
	// ErrVESchema is a MEDIA_UNAVAILABLE case: migration 024 has not been applied.
	ErrVESchema = fmt.Errorf("video editor schema missing migration 024: %w", ErrVEMedia)
)

type videoEditorStorage interface {
	Upload(context.Context, string, []byte, string) error
	SignedURL(context.Context, string, time.Duration) (string, time.Time, error)
	SignedDownloadURL(context.Context, string, time.Duration, string) (string, time.Time, error)
}
type VideoEditorFacade struct {
	repo      *repo.VideoEditorRepository
	storage   videoEditorStorage
	processor videoprocessing.VideoProcessor
	client    *http.Client
	available bool
	// Text capability: success is cached forever, failure for veTextRetry.
	textMu      sync.Mutex
	textReady   bool
	textChecked time.Time
	textRetry   time.Duration // tests only; zero means veTextRetry
	schemaOK    atomic.Bool
	imageOK     atomic.Bool
}

const (
	veMaxAudioClips  = 20
	veMaxTextLayers  = 50
	veMaxImageLayers = 20
	veMaxTimeline    = 1200.0
)

// thaiTextChecker is implemented by renderers that can burn Thai captions.
type thaiTextChecker interface {
	SupportsThaiTextOverlays(context.Context) bool
}

// veTextRetry bounds how long a failed text-capability probe is reused, so a
// transient FFmpeg/font problem does not disable text export until restart.
const veTextRetry = 60 * time.Second

// TextOverlays is cached because it shells out to FFmpeg and reads the font.
// Success is cached for the process lifetime; failure is re-probed after
// veTextRetry. The constructor warms the cache in the background.
func (f *VideoEditorFacade) TextOverlays() bool {
	c, ok := f.processor.(thaiTextChecker)
	if !f.available || !ok {
		return false
	}
	f.textMu.Lock()
	defer f.textMu.Unlock()
	retry := veTextRetry
	if f.textRetry > 0 {
		retry = f.textRetry
	}
	if f.textReady || (!f.textChecked.IsZero() && time.Since(f.textChecked) < retry) {
		return f.textReady
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f.textReady = c.SupportsThaiTextOverlays(ctx)
	f.textChecked = time.Now()
	return f.textReady
}

// schemaReady fails with ErrVESchema until migration 024's columns exist; success
// is cached, so the check costs one catalog query per process once migrated.
// imageReady reports migration 028; success is cached like schemaReady. Without a
// repository (unit tests) the schema is assumed current.
func (f *VideoEditorFacade) imageReady(ctx context.Context) bool {
	if f.imageOK.Load() || f.repo == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !f.repo.ImageLayersReady(ctx) {
		return false
	}
	f.imageOK.Store(true)
	return true
}

func (f *VideoEditorFacade) schemaReady(ctx context.Context) error {
	if f.schemaOK.Load() || f.repo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !f.repo.AudioTextReady(ctx) {
		return ErrVESchema
	}
	f.schemaOK.Store(true)
	return nil
}

func NewVideoEditorFacade(r *repo.VideoEditorRepository, s videoEditorStorage, p videoprocessing.VideoProcessor) *VideoEditorFacade {
	f := &VideoEditorFacade{repo: r, storage: s, processor: p, client: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, available: s != nil && p != nil && videoeditor.Available()}
	if f.available {
		go f.TextOverlays()
	}
	return f
}
func (f *VideoEditorFacade) Capabilities() dto.VideoEditorCapabilitiesDTO {
	reason := ""
	available := f.available
	if !available {
		reason = "ระบบตัดต่อยังไม่พร้อมใช้งาน กรุณาตรวจการตั้งค่า storage และ FFmpeg"
	} else if f.schemaReady(context.Background()) != nil {
		available = false
		reason = "ระบบตัดต่อยังไม่พร้อมใช้งาน ฐานข้อมูลยังไม่ได้อัปเดต (migration 024)"
	}
	return dto.VideoEditorCapabilitiesDTO{Available: available, UnavailableReason: reason, MaxUploadBytes: videoeditor.MaxUploadBytes, MaxSegments: 20, MaxDuration: 1200, AspectRatios: []string{"9:16", "16:9", "1:1"}, Resolutions: []string{"720p", "1080p"}, MaxAudioClips: veMaxAudioClips, MaxTextLayers: veMaxTextLayers, TextOverlays: available && f.TextOverlays(), TextTransforms: true, AudioFormats: []string{"mp3", "wav", "m4a"}, ClipEffects: true, ClipStyles: true, ImageLayers: available && f.imageReady(context.Background()), MaxImageLayers: veMaxImageLayers, ImageFormats: []string{"png", "jpg", "webp"}}
}
func veID(v string) bool    { _, e := uuid.Parse(v); return e == nil }
func veTitle(v string) bool { return strings.TrimSpace(v) != "" && utf8.RuneCountInString(v) <= 200 }
func veHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func veSegments(p *models.VideoEditorProject) []models.VideoEditorSegment {
	out := []models.VideoEditorSegment{}
	_ = json.Unmarshal(p.Segments, &out)
	if out == nil {
		out = []models.VideoEditorSegment{}
	}
	return out
}
func veAudioClips(p *models.VideoEditorProject) []models.VideoEditorAudioClip {
	out := []models.VideoEditorAudioClip{}
	_ = json.Unmarshal(p.AudioClips, &out)
	if out == nil {
		out = []models.VideoEditorAudioClip{}
	}
	return out
}
func veTextLayers(p *models.VideoEditorProject) []models.VideoEditorTextLayer {
	out := []models.VideoEditorTextLayer{}
	_ = json.Unmarshal(p.TextLayers, &out)
	if out == nil {
		out = []models.VideoEditorTextLayer{}
	}
	return out
}
func veImageLayers(p *models.VideoEditorProject) []models.VideoEditorImageLayer {
	out := []models.VideoEditorImageLayer{}
	_ = json.Unmarshal(p.ImageLayers, &out)
	if out == nil {
		out = []models.VideoEditorImageLayer{}
	}
	return out
}
func veProjectDTO(p *models.VideoEditorProject) dto.VideoEditorProjectDTO {
	return dto.VideoEditorProjectDTO{ID: p.ID.String(), Title: p.Title, AspectRatio: p.AspectRatio, Revision: p.Revision, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339), Segments: veSegments(p), AudioClips: veAudioClips(p), TextLayers: veTextLayers(p), ImageLayers: veImageLayers(p), Assets: []dto.VideoEditorAssetDTO{}}
}
func veKind(a *models.VideoEditorAsset) string {
	if a.Kind == "audio" || a.Kind == "image" {
		return a.Kind
	}
	return "video"
}

// validateVEImageLayers: ready owned image assets, inside the timeline, centre in the frame,
// width 2..100% of the frame, opacity 0.05..1, at most 20.
func validateVEImageLayers(layers []models.VideoEditorImageLayer, assets []models.VideoEditorAsset) error {
	if len(layers) > veMaxImageLayers {
		return ErrVEInvalid
	}
	owned := map[string]bool{}
	for _, a := range assets {
		if a.Status == "ready" && veKind(&a) == "image" {
			owned[a.ID.String()] = true
		}
	}
	seen := map[string]bool{}
	for _, l := range layers {
		for _, v := range []float64{l.Start, l.End, l.X, l.Y, l.Width, l.Opacity} {
			if !veFinite(v) {
				return ErrVEInvalid
			}
		}
		if !veID(l.ID) || seen[l.ID] || !owned[l.AssetID] || l.Start < 0 || l.Start >= l.End || l.End > veMaxTimeline || l.X < 0 || l.X > 1 || l.Y < 0 || l.Y > 1 || l.Width < .02 || l.Width > 1 || l.Opacity < .05 || l.Opacity > 1 {
			return ErrVEInvalid
		}
		seen[l.ID] = true
	}
	return nil
}
func (f *VideoEditorFacade) CreateProject(ctx context.Context, user string, in req.CreateVideoEditorProject) (dto.VideoEditorProjectDTO, bool, error) {
	if !veID(user) || !veID(in.RequestID) || !veTitle(in.Title) {
		return dto.VideoEditorProjectDTO{}, false, ErrVEInvalid
	}
	if e := f.schemaReady(ctx); e != nil {
		return dto.VideoEditorProjectDTO{}, false, e
	}
	p := models.VideoEditorProject{UserID: uuid.MustParse(user), RequestID: uuid.MustParse(in.RequestID), RequestHash: veHash(in), Title: in.Title, AspectRatio: "9:16", Revision: 1, Segments: datatypes.JSON("[]"), AudioClips: datatypes.JSON("[]"), TextLayers: datatypes.JSON("[]")}
	created, e := f.repo.CreateProject(ctx, &p)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, false, e
	}
	d, e := f.Project(ctx, p.ID.String(), user)
	return d, created, e
}
func (f *VideoEditorFacade) Projects(ctx context.Context, user string, page, limit int) (dto.VideoEditorProjectListDTO, error) {
	ps, n, e := f.repo.Projects(ctx, user, page, limit)
	d := dto.VideoEditorProjectListDTO{Items: []dto.VideoEditorProjectDTO{}, Total: n, Page: page, Limit: limit}
	for i := range ps {
		d.Items = append(d.Items, veProjectDTO(&ps[i]))
	}
	return d, e
}
func (f *VideoEditorFacade) Project(ctx context.Context, id, user string) (dto.VideoEditorProjectDTO, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorProjectDTO{}, repo.ErrNotFound
	}
	p, e := f.repo.Project(ctx, id, user)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	d := veProjectDTO(p)
	d.Assets, e = f.Assets(ctx, id, user)
	return d, e
}
func (f *VideoEditorFacade) Assets(ctx context.Context, id, user string) ([]dto.VideoEditorAssetDTO, error) {
	if !veID(id) || !veID(user) {
		return nil, repo.ErrNotFound
	}
	if _, e := f.repo.Project(ctx, id, user); e != nil {
		return nil, e
	}
	as, e := f.repo.Assets(ctx, id, user)
	if e != nil {
		return nil, e
	}
	out := make([]dto.VideoEditorAssetDTO, len(as))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	jobs := make(chan int)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				d, e := f.assetDTO(ctx, &as[i])
				if e != nil {
					mu.Lock()
					if first == nil {
						first = e
						cancel()
					}
					mu.Unlock()
				}
				out[i] = d
			}
		}()
	}
	for i := range as {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)
	wg.Wait()
	return out, first
}
func (f *VideoEditorFacade) assetDTO(ctx context.Context, a *models.VideoEditorAsset) (dto.VideoEditorAssetDTO, error) {
	d := dto.VideoEditorAssetDTO{ID: a.ID.String(), ProjectID: a.ProjectID.String(), ClientUploadID: a.ClientUploadID.String(), Name: a.Name, Kind: veKind(a), Status: a.Status, Duration: a.Duration, Width: a.Width, Height: a.Height, HasAudio: a.HasAudio, SizeBytes: a.SizeBytes, Frames: []dto.VideoEditorFrameDTO{}, FailureCode: a.FailureCode, FailureMessage: a.FailureMessage}
	if a.Status == "processing" && a.LeaseUntil != nil && a.LeaseUntil.Before(time.Now()) {
		d.Status = "failed"
		d.FailureCode = "UPLOAD_INTERRUPTED"
		d.FailureMessage = "การอัปโหลดหยุดชะงัก กรุณาเลือกไฟล์เดิมเพื่อลองใหม่"
	}
	if a.Status != "ready" {
		return d, nil
	}
	if f.storage == nil {
		return d, ErrVEStorage
	}
	u, expiry, e := f.storage.SignedURL(ctx, a.SourceKey, time.Hour)
	if e != nil {
		return d, ErrVEStorage
	}
	d.PreviewURL = &u
	frames := []models.VideoEditorFrame{}
	if json.Unmarshal(a.Frames, &frames) != nil {
		return d, ErrVEStorage
	}
	for _, frame := range frames {
		u, exp, e := f.storage.SignedURL(ctx, frame.Key, time.Hour)
		if e != nil {
			return d, ErrVEStorage
		}
		if exp.Before(expiry) {
			expiry = exp
		}
		d.Frames = append(d.Frames, dto.VideoEditorFrameDTO{Time: frame.Time, URL: u})
	}
	s := expiry.UTC().Format(time.RFC3339)
	d.ExpiresAt = &s
	return d, nil
}
func validateVESegments(segments []models.VideoEditorSegment, assets []models.VideoEditorAsset) error {
	if len(segments) > 20 {
		return ErrVEInvalid
	}
	owned := map[string]models.VideoEditorAsset{}
	for _, a := range assets {
		if a.Status == "ready" && veKind(&a) == "video" {
			owned[a.ID.String()] = a
		}
	}
	seen := map[string]bool{}
	total := 0.0
	for i, s := range segments {
		a, ok := owned[s.AssetID]
		if !veID(s.ID) || seen[s.ID] || !ok || !veFinite(s.Start) || !veFinite(s.End) || !veFinite(s.Volume) || s.Start < 0 || s.Start >= s.End || s.End > a.Duration+.001 || s.Volume < 0 || s.Volume > 1 {
			return ErrVEInvalid
		}
		if !veFinite(s.Speed) || s.Speed != 0 && (s.Speed < veMinSpeed || s.Speed > veMaxSpeed) || !veValidFades(s.FadeIn, s.FadeOut, s.TimelineLength()) || !veValidLook(s.VideoEditorClipLook) {
			return ErrVEInvalid
		}
		if !veValidTransition(segments, i) {
			return ErrVEInvalid
		}
		seen[s.ID] = true
		total += s.TimelineLength() - s.TransitionDuration
	}
	if total > 1200 {
		return ErrVEInvalid
	}
	return nil
}
func veFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Speed range matches the editor UI; atempo is chained so every factor stays within 0.5–2.
const (
	veMinSpeed = 0.25
	veMaxSpeed = 4
	veMaxFade  = 10
)

// veValidFades: each fade is 0..10 s and the two together fit inside the item.
func veValidFades(in, out, length float64) bool {
	return veFinite(in) && veFinite(out) && in >= 0 && out >= 0 && in <= veMaxFade && out <= veMaxFade && in+out <= length+.001
}

// Transition names are FFmpeg xfade transitions the preview can reproduce.
var veTransitions = map[string]bool{"fade": true, "fadeblack": true, "fadewhite": true, "wipeleft": true, "wiperight": true, "slideleft": true, "slideright": true, "circleopen": true}

// veValidTransition: none on the first segment; otherwise a known type lasting 0.1..2 s
// and at most half of this and the previous segment, so neighbouring transitions never meet.
func veValidTransition(segments []models.VideoEditorSegment, i int) bool {
	s := segments[i]
	if s.Transition == "" {
		return s.TransitionDuration == 0
	}
	if i == 0 || !veTransitions[s.Transition] || !veFinite(s.TransitionDuration) || s.TransitionDuration < 0.1 || s.TransitionDuration > 2 {
		return false
	}
	return s.TransitionDuration <= math.Min(s.TimelineLength(), segments[i-1].TimelineLength())/2+.001
}

// veValidLook: colour adjustments and pan are -1..1, zoom 1..3 (0 = 1), fit "" or "cover".
func veValidLook(l models.VideoEditorClipLook) bool {
	for _, v := range []float64{l.Brightness, l.Contrast, l.Saturation, l.PanX, l.PanY} {
		if !veFinite(v) || v < -1 || v > 1 {
			return false
		}
	}
	return veFinite(l.Zoom) && (l.Zoom == 0 || l.Zoom >= 1 && l.Zoom <= 3) && (l.Fit == "" || l.Fit == "cover")
}

// veAudioSource: audio clips may use an audio asset, or the sound track of a video
// asset that has one (a detached clip sound).
func veAudioSource(a *models.VideoEditorAsset) bool {
	return a.Status == "ready" && (veKind(a) == "audio" || veKind(a) == "video" && a.HasAudio)
}
func validateVEAudioClips(clips []models.VideoEditorAudioClip, assets []models.VideoEditorAsset) error {
	if len(clips) > veMaxAudioClips {
		return ErrVEInvalid
	}
	owned := map[string]models.VideoEditorAsset{}
	for _, a := range assets {
		if veAudioSource(&a) {
			owned[a.ID.String()] = a
		}
	}
	seen := map[string]bool{}
	for _, c := range clips {
		a, ok := owned[c.AssetID]
		if !veID(c.ID) || c.OriginJobID != "" && !veID(c.OriginJobID) || seen[c.ID] || !ok || !veFinite(c.At) || !veFinite(c.Start) || !veFinite(c.End) || !veFinite(c.Volume) || c.At < 0 || c.Start < 0 || c.Start >= c.End || c.End > a.Duration+.001 || c.Volume < 0 || c.Volume > 1 || c.At+(c.End-c.Start) > veMaxTimeline+.001 || !veValidFades(c.FadeIn, c.FadeOut, c.End-c.Start) {
			return ErrVEInvalid
		}
		seen[c.ID] = true
	}
	return nil
}

var veColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// veTextRejected lists runes the renderer cannot show the way the preview does:
// control characters other than LF (including tab), bidi embedding/override and
// isolate controls, zero-width characters/BOM, and Unicode line/paragraph
// separators (only LF breaks lines).
func veTextRejected(r rune) bool {
	switch {
	case r == '\n':
		return false
	case unicode.IsControl(r):
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0x200B && r <= 0x200D, r == 0x2060, r == 0xFEFF:
		return true
	case r == 0x2028, r == 0x2029:
		return true
	}
	return false
}

// normalizeVETextLayers validates layers and returns them with text normalized to
// LF line endings and trimmed, which is the stored and rendered form.
func normalizeVETextLayers(layers []models.VideoEditorTextLayer) ([]models.VideoEditorTextLayer, error) {
	if len(layers) > veMaxTextLayers {
		return nil, ErrVEInvalid
	}
	out := make([]models.VideoEditorTextLayer, 0, len(layers))
	seen := map[string]bool{}
	for _, l := range layers {
		text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(l.Text, "\r\n", "\n"), "\r", "\n"))
		if !veID(l.ID) || l.OriginJobID != "" && !veID(l.OriginJobID) || seen[l.ID] || !utf8.ValidString(text) || text == "" || utf8.RuneCountInString(text) > 500 || strings.Count(text, "\n") > 5 || strings.ContainsFunc(text, veTextRejected) {
			return nil, ErrVEInvalid
		}
		if !veFinite(l.Start) || !veFinite(l.End) || !veFinite(l.X) || !veFinite(l.Y) || l.Start < 0 || l.Start >= l.End || l.End > veMaxTimeline || l.X < 0 || l.X > 1 || l.Y < 0 || l.Y > 1 || l.FontSize < 12 || l.FontSize > 200 || !veColor.MatchString(l.Color) || (l.Align != "left" && l.Align != "center" && l.Align != "right") {
			return nil, ErrVEInvalid
		}
		for _, scale := range []*float64{l.ScaleX, l.ScaleY} {
			if scale != nil && (!veFinite(*scale) || *scale < 0.1 || *scale > 5) {
				return nil, ErrVEInvalid
			}
		}
		if l.OutlineWidth < 0 || l.OutlineWidth > 20 || l.OutlineWidth > 0 && !veColor.MatchString(l.OutlineColor) || l.OutlineWidth == 0 && l.OutlineColor != "" ||
			!veFinite(l.BoxOpacity) || l.BoxOpacity < 0 || l.BoxOpacity > 1 || l.BoxOpacity > 0 && !veColor.MatchString(l.BoxColor) || l.BoxOpacity == 0 && l.BoxColor != "" ||
			l.Animation != "" && l.Animation != "fade" && l.Animation != "slide" {
			return nil, ErrVEInvalid
		}
		seen[l.ID] = true
		l.Text = text
		out = append(out, l)
	}
	return out, nil
}
func (f *VideoEditorFacade) SaveProject(ctx context.Context, id, user string, in req.SaveVideoEditorProject) (dto.VideoEditorProjectDTO, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorProjectDTO{}, repo.ErrNotFound
	}
	if !veTitle(in.Title) || in.Revision < 1 || (in.AspectRatio != "9:16" && in.AspectRatio != "16:9" && in.AspectRatio != "1:1") {
		return dto.VideoEditorProjectDTO{}, ErrVEInvalid
	}
	if e := f.schemaReady(ctx); e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	p, e := f.repo.Project(ctx, id, user)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	as, e := f.repo.Assets(ctx, id, user)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	if e = validateVESegments(in.Segments, as); e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	if in.Segments == nil {
		in.Segments = []models.VideoEditorSegment{}
	}
	b, e := json.Marshal(in.Segments)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, ErrVEInvalid
	}
	audio, text := veAudioClips(p), veTextLayers(p)
	if in.AudioClips != nil {
		audio = *in.AudioClips
		if audio == nil {
			audio = []models.VideoEditorAudioClip{}
		}
		if e = validateVEAudioClips(audio, as); e != nil {
			return dto.VideoEditorProjectDTO{}, e
		}
	}
	if in.TextLayers != nil {
		if text, e = normalizeVETextLayers(*in.TextLayers); e != nil {
			return dto.VideoEditorProjectDTO{}, e
		}
	}
	if in.ImageLayers != nil {
		if !f.imageReady(ctx) {
			return dto.VideoEditorProjectDTO{}, ErrVESchema
		}
		images := *in.ImageLayers
		if images == nil {
			images = []models.VideoEditorImageLayer{}
		}
		if e = validateVEImageLayers(images, as); e != nil {
			return dto.VideoEditorProjectDTO{}, e
		}
		raw, e := json.Marshal(images)
		if e != nil {
			return dto.VideoEditorProjectDTO{}, ErrVEInvalid
		}
		p.ImageLayers = datatypes.JSON(raw)
	}
	audioRaw, e := json.Marshal(audio)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, ErrVEInvalid
	}
	textRaw, e := json.Marshal(text)
	if e != nil {
		return dto.VideoEditorProjectDTO{}, ErrVEInvalid
	}
	p.AudioClips = datatypes.JSON(audioRaw)
	p.TextLayers = datatypes.JSON(textRaw)
	p.Title = in.Title
	p.AspectRatio = in.AspectRatio
	p.Segments = datatypes.JSON(b)
	if e = f.repo.Save(ctx, p, in.Revision); e != nil {
		return dto.VideoEditorProjectDTO{}, e
	}
	return f.Project(ctx, id, user)
}
func (f *VideoEditorFacade) Upload(ctx context.Context, id, user, uploadID, name, mime string, data []byte) (dto.VideoEditorAssetDTO, bool, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorAssetDTO{}, false, repo.ErrNotFound
	}
	if _, e := f.repo.Project(ctx, id, user); e != nil {
		return dto.VideoEditorAssetDTO{}, false, e
	}
	if !f.available {
		return dto.VideoEditorAssetDTO{}, false, ErrVEMedia
	}
	if e := f.schemaReady(ctx); e != nil {
		return dto.VideoEditorAssetDTO{}, false, e
	}
	if !veID(uploadID) || len(data) == 0 {
		return dto.VideoEditorAssetDTO{}, false, ErrVEInvalid
	}
	if int64(len(data)) > videoeditor.MaxUploadBytes {
		return dto.VideoEditorAssetDTO{}, false, ErrVESize
	}
	if !videoeditor.Magic(data) || !veUploadMIME[mime] {
		return dto.VideoEditorAssetDTO{}, false, ErrVEType
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if !veTitle(name) {
		return dto.VideoEditorAssetDTO{}, false, ErrVEInvalid
	}
	h := sha256.Sum256(data)
	lease := time.Now().Add(130 * time.Second)
	a := models.VideoEditorAsset{UserID: uuid.MustParse(user), ProjectID: uuid.MustParse(id), ClientUploadID: uuid.MustParse(uploadID), ContentHash: hex.EncodeToString(h[:]), Name: name, Kind: "video", Status: "processing", SizeBytes: int64(len(data)), Frames: datatypes.JSON("[]"), AttemptToken: uuid.New(), LeaseUntil: &lease}
	reserved, e := f.repo.ReserveAsset(ctx, &a)
	if e != nil {
		return dto.VideoEditorAssetDTO{}, false, e
	}
	if !reserved {
		d, e := f.assetDTO(ctx, &a)
		return d, false, e
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	fail := func(err error) (dto.VideoEditorAssetDTO, bool, error) {
		code := "STORAGE_UNAVAILABLE"
		message := "บันทึกไฟล์ไม่สำเร็จ กรุณาลองใหม่"
		if errors.Is(err, ErrVEType) {
			code = "INVALID_FILE_TYPE"
			message = "ไม่สามารถอ่านไฟล์วิดีโอหรือเสียงนี้ได้"
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.repo.FailAsset(cleanup, a.ID.String(), user, a.AttemptToken, code, message)
		return dto.VideoEditorAssetDTO{}, false, err
	}
	dir, e := os.MkdirTemp("", "video-editor-upload-*")
	if e != nil {
		return fail(ErrVEMedia)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "source")
	if e = os.WriteFile(path, data, 0600); e != nil {
		return fail(ErrVEMedia)
	}
	media, e := videoeditor.Inspect(ctx, path)
	if e != nil {
		return fail(ErrVEType)
	}
	if !veMIMECompatible(media.MIME, mime) {
		return fail(ErrVEType)
	}
	if media.Kind == "image" && !f.imageReady(ctx) {
		return fail(ErrVESchema)
	}
	prefix := fmt.Sprintf("video-editor/%s/%s/assets/%s/%s", user, id, a.ID, a.AttemptToken)
	a.SourceKey = prefix + "/source"
	if e = f.storage.Upload(ctx, a.SourceKey, data, media.MIME); e != nil {
		return fail(ErrVEStorage)
	}
	frames := []models.VideoEditorFrame{}
	for i, frame := range media.Frames {
		key := fmt.Sprintf("%s/frame-%02d.jpg", prefix, i)
		if e = f.storage.Upload(ctx, key, frame.Data, "image/jpeg"); e != nil {
			return fail(ErrVEStorage)
		}
		frames = append(frames, models.VideoEditorFrame{Time: frame.Time, Key: key})
	}
	raw, e := json.Marshal(frames)
	if e != nil {
		return fail(ErrVEStorage)
	}
	a.Kind = media.Kind
	a.Duration = media.Duration
	a.Width = media.Width
	a.Height = media.Height
	a.HasAudio = media.HasAudio
	a.Frames = datatypes.JSON(raw)
	if e = f.repo.CompleteAsset(ctx, &a); e != nil {
		return fail(e)
	}
	d, e := f.assetDTO(ctx, &a)
	return d, true, e
}

var veUploadMIME = map[string]bool{"video/mp4": true, "video/quicktime": true, "video/webm": true, "application/octet-stream": true, "audio/mpeg": true, "audio/mp3": true, "audio/wav": true, "audio/x-wav": true, "audio/wave": true, "audio/vnd.wave": true, "audio/mp4": true, "audio/x-m4a": true, "audio/m4a": true, "audio/aac": true, "image/png": true, "image/jpeg": true, "image/webp": true}

// veMIMECompatible checks the declared type against the inspected container; the
// declared type is never trusted on its own.
func veMIMECompatible(detected, declared string) bool {
	if declared == "application/octet-stream" {
		return true
	}
	switch detected {
	case "video/webm":
		return declared == "video/webm"
	case "video/mp4":
		return declared == "video/mp4" || declared == "video/quicktime"
	case "audio/mpeg":
		return declared == "audio/mpeg" || declared == "audio/mp3"
	case "audio/wav":
		return declared == "audio/wav" || declared == "audio/x-wav" || declared == "audio/wave" || declared == "audio/vnd.wave"
	case "image/png", "image/jpeg", "image/webp":
		return declared == detected
	case "audio/mp4":
		return declared == "audio/mp4" || declared == "audio/x-m4a" || declared == "audio/m4a" || declared == "audio/aac" || declared == "video/mp4" || declared == "video/quicktime"
	}
	return false
}
func veExportDTO(e *models.VideoEditorExport) dto.VideoEditorExportDTO {
	return dto.VideoEditorExportDTO{ID: e.ID.String(), ProjectID: e.ProjectID.String(), Revision: e.Revision, Status: e.Status, Resolution: e.Resolution, AspectRatio: e.AspectRatio, Duration: e.Duration, CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339), FailureCode: e.FailureCode, FailureMessage: e.FailureMessage, CanRetry: e.Status == "failed" && e.Attempts < 3}
}
func (f *VideoEditorFacade) CreateExport(ctx context.Context, id, user string, in req.CreateVideoEditorExport) (dto.VideoEditorExportDTO, bool, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorExportDTO{}, false, repo.ErrNotFound
	}
	if !veID(in.RequestID) || in.Revision < 1 || (in.Resolution != "720p" && in.Resolution != "1080p") {
		return dto.VideoEditorExportDTO{}, false, ErrVEInvalid
	}
	hash := veHash(struct {
		ProjectID string
		Request   req.CreateVideoEditorExport
	}{id, in})
	old, e := f.repo.ExistingExport(ctx, user, in.RequestID)
	if e == nil {
		if old.RequestHash != hash {
			return dto.VideoEditorExportDTO{}, false, repo.ErrVEIdempotency
		}
		return veExportDTO(old), false, nil
	}
	if !errors.Is(e, repo.ErrNotFound) {
		return dto.VideoEditorExportDTO{}, false, e
	}
	if !f.available {
		return dto.VideoEditorExportDTO{}, false, ErrVEMedia
	}
	if e := f.schemaReady(ctx); e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	p, e := f.repo.Project(ctx, id, user)
	if e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	if p.Revision != in.Revision {
		return dto.VideoEditorExportDTO{}, false, repo.ErrVERevision
	}
	segments := veSegments(p)
	if len(segments) == 0 {
		return dto.VideoEditorExportDTO{}, false, ErrVEInvalid
	}
	assets, e := f.repo.Assets(ctx, id, user)
	if e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	if e = validateVESegments(segments, assets); e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	audio := veAudioClips(p)
	if e = validateVEAudioClips(audio, assets); e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	text, e := normalizeVETextLayers(veTextLayers(p))
	if e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	images := veImageLayers(p)
	if e = validateVEImageLayers(images, assets); e != nil {
		return dto.VideoEditorExportDTO{}, false, e
	}
	snapshot, duration := buildVESnapshot(p.AspectRatio, in.Resolution, segments, audio, text, assets)
	snapshot.ImageLayers = veSnapshotImages(images, assets, duration)
	if len(snapshot.TextLayers) > 0 && !f.TextOverlays() {
		return dto.VideoEditorExportDTO{}, false, ErrVETextUnavailable
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return dto.VideoEditorExportDTO{}, false, ErrVEInvalid
	}
	exp := models.VideoEditorExport{UserID: p.UserID, ProjectID: p.ID, RequestID: uuid.MustParse(in.RequestID), RequestHash: hash, Revision: in.Revision, Status: "queued", Resolution: in.Resolution, AspectRatio: p.AspectRatio, Duration: duration, Snapshot: datatypes.JSON(raw)}
	created, e := f.repo.CreateExport(ctx, &exp)
	return veExportDTO(&exp), created, e
}

// veMinSpan drops audio clips/text layers with less than 1 ms left after clipping.
// The nanosecond tolerance preserves exact millisecond boundaries after float subtraction.
const veMinSpan = 0.001

// buildVESnapshot freezes the timeline. Output length is the video total: audio is
// cut at the end and text is clipped, with items starting at/after the end, or
// with less than veMinSpan remaining, dropped.
func buildVESnapshot(ratio, resolution string, segments []models.VideoEditorSegment, audio []models.VideoEditorAudioClip, text []models.VideoEditorTextLayer, assets []models.VideoEditorAsset) (models.VideoEditorSnapshot, float64) {
	byID := map[string]models.VideoEditorAsset{}
	for _, a := range assets {
		byID[a.ID.String()] = a
	}
	snapshot := models.VideoEditorSnapshot{AspectRatio: ratio, Resolution: resolution, Clips: []models.VideoEditorSnapshotClip{}, AudioClips: []models.VideoEditorSnapshotAudio{}, TextLayers: []models.VideoEditorTextLayer{}}
	total := 0.0
	for _, s := range segments {
		snapshot.Clips = append(snapshot.Clips, models.VideoEditorSnapshotClip{AssetID: s.AssetID, Key: byID[s.AssetID].SourceKey, Start: s.Start, End: s.End, Volume: s.Volume, Speed: s.Speed, FadeIn: s.FadeIn, FadeOut: s.FadeOut, VideoEditorClipLook: s.VideoEditorClipLook, VideoEditorTransition: s.VideoEditorTransition})
		total += s.TimelineLength() - s.TransitionDuration
	}
	for _, c := range audio {
		if c.At >= total {
			continue
		}
		end := math.Min(c.End, c.Start+(total-c.At))
		if end-c.Start < veMinSpan-1e-9 {
			continue
		}
		// A clip cut at the video end keeps its fade-in; the fade-out shrinks to fit what remains.
		fadeIn := math.Min(c.FadeIn, end-c.Start)
		fadeOut := math.Min(c.FadeOut, end-c.Start-fadeIn)
		snapshot.AudioClips = append(snapshot.AudioClips, models.VideoEditorSnapshotAudio{AssetID: c.AssetID, Key: byID[c.AssetID].SourceKey, At: c.At, Start: c.Start, End: end, Volume: c.Volume, FadeIn: fadeIn, FadeOut: fadeOut})
	}
	for _, l := range text {
		if l.Start >= total {
			continue
		}
		l.End = math.Min(l.End, total)
		if l.End-l.Start < veMinSpan-1e-9 {
			continue
		}
		snapshot.TextLayers = append(snapshot.TextLayers, l)
	}
	return snapshot, total
}
func (f *VideoEditorFacade) Exports(ctx context.Context, id, user string) ([]dto.VideoEditorExportDTO, error) {
	if !veID(id) || !veID(user) {
		return nil, repo.ErrNotFound
	}
	if _, e := f.repo.Project(ctx, id, user); e != nil {
		return nil, e
	}
	if e := f.repo.ExpireExports(ctx); e != nil {
		return nil, e
	}
	es, e := f.repo.Exports(ctx, id, user)
	out := []dto.VideoEditorExportDTO{}
	for i := range es {
		out = append(out, veExportDTO(&es[i]))
	}
	return out, e
}
func (f *VideoEditorFacade) Export(ctx context.Context, id, user string) (dto.VideoEditorExportDTO, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorExportDTO{}, repo.ErrNotFound
	}
	if err := f.repo.ExpireExports(ctx); err != nil {
		return dto.VideoEditorExportDTO{}, err
	}
	e, err := f.repo.Export(ctx, id, user)
	if err != nil {
		return dto.VideoEditorExportDTO{}, err
	}
	return veExportDTO(e), nil
}
func (f *VideoEditorFacade) RetryExport(ctx context.Context, id, user string) (dto.VideoEditorExportDTO, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorExportDTO{}, repo.ErrNotFound
	}
	if !f.available {
		return dto.VideoEditorExportDTO{}, ErrVEMedia
	}
	if err := f.repo.ExpireExports(ctx); err != nil {
		return dto.VideoEditorExportDTO{}, err
	}
	if err := f.repo.Retry(ctx, id, user); err != nil {
		return dto.VideoEditorExportDTO{}, err
	}
	return f.Export(ctx, id, user)
}
func (f *VideoEditorFacade) Download(ctx context.Context, id, user string) (dto.VideoEditorDownloadDTO, error) {
	if !veID(id) || !veID(user) {
		return dto.VideoEditorDownloadDTO{}, repo.ErrNotFound
	}
	e, err := f.repo.Export(ctx, id, user)
	if err != nil {
		return dto.VideoEditorDownloadDTO{}, err
	}
	if e.Status != "completed" || e.OutputKey == "" {
		return dto.VideoEditorDownloadDTO{}, repo.ErrVEExportState
	}
	filename := "video-" + e.ID.String() + ".mp4"
	u, exp, err := f.storage.SignedDownloadURL(ctx, e.OutputKey, time.Hour, filename)
	if err != nil {
		return dto.VideoEditorDownloadDTO{}, ErrVEStorage
	}
	return dto.VideoEditorDownloadDTO{DownloadURL: u, ExpiresAt: exp.UTC().Format(time.RFC3339), Filename: filename}, nil
}
func (f *VideoEditorFacade) ProcessNext(ctx context.Context) (bool, error) {
	if !f.available {
		return false, ErrVEMedia
	}
	if err := f.repo.ExpireExports(ctx); err != nil {
		return false, err
	}
	e, err := f.repo.Claim(ctx)
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	key, err := f.render(ctx, e)
	code, message := "", ""
	if err != nil {
		code = "RENDER_FAILED"
		message = "ส่งออกไม่สำเร็จ กรุณาลองใหม่"
	}
	finish, cancelFinish := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFinish()
	if finishErr := f.repo.FinishExport(finish, e, key, code, message); finishErr != nil {
		return true, finishErr
	}
	return true, err
}

// veSnapshotImages clips image layers to the video like text, dropping those that start at/after the end.
func veSnapshotImages(layers []models.VideoEditorImageLayer, assets []models.VideoEditorAsset, total float64) []models.VideoEditorSnapshotImage {
	keys := map[string]string{}
	for _, a := range assets {
		keys[a.ID.String()] = a.SourceKey
	}
	out := []models.VideoEditorSnapshotImage{}
	for _, l := range layers {
		end := math.Min(l.End, total)
		if l.Start >= total || end-l.Start < veMinSpan-1e-9 {
			continue
		}
		out = append(out, models.VideoEditorSnapshotImage{AssetID: l.AssetID, Key: keys[l.AssetID], Start: l.Start, End: end, X: l.X, Y: l.Y, Width: l.Width, Opacity: l.Opacity})
	}
	return out
}
func (f *VideoEditorFacade) render(ctx context.Context, e *models.VideoEditorExport) (string, error) {
	var snap models.VideoEditorSnapshot
	if json.Unmarshal(e.Snapshot, &snap) != nil {
		return "", ErrVEInvalid
	}
	dir, err := os.MkdirTemp("", "video-editor-render-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	paths := map[string]string{}
	fetch := func(key string) (string, error) {
		if path := paths[key]; path != "" {
			return path, nil
		}
		u, _, err := f.storage.SignedURL(ctx, key, time.Hour)
		if err != nil {
			return "", ErrVEStorage
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", ErrVEStorage
		}
		response, err := f.client.Do(request)
		if err != nil {
			return "", ErrVEStorage
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return "", ErrVEStorage
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, videoeditor.MaxUploadBytes+1))
		response.Body.Close()
		if err != nil || int64(len(data)) > videoeditor.MaxUploadBytes {
			return "", ErrVEStorage
		}
		path := filepath.Join(dir, fmt.Sprintf("source-%d", len(paths)))
		if err = os.WriteFile(path, data, 0600); err != nil {
			return "", err
		}
		paths[key] = path
		return path, nil
	}
	request := videoprocessing.MergeRequest{OutputResolution: snap.Resolution, OutputAspectRatio: snap.AspectRatio}
	for _, c := range snap.Clips {
		path, err := fetch(c.Key)
		if err != nil {
			return "", err
		}
		volume := c.Volume
		request.Clips = append(request.Clips, videoprocessing.Clip{Path: path, TrimStart: c.Start, TrimEnd: c.End, Volume: &volume, Speed: c.Speed, FadeIn: c.FadeIn, FadeOut: c.FadeOut, Look: videoprocessing.ClipLook(c.VideoEditorClipLook), Transition: c.Transition, TransitionDuration: c.TransitionDuration})
	}
	for _, c := range snap.AudioClips {
		path, err := fetch(c.Key)
		if err != nil {
			return "", err
		}
		request.AudioTracks = append(request.AudioTracks, videoprocessing.AudioTrack{Path: path, TrimStart: c.Start, TrimEnd: c.End, At: c.At, Volume: c.Volume, FadeIn: c.FadeIn, FadeOut: c.FadeOut})
	}
	for _, l := range snap.ImageLayers {
		path, err := fetch(l.Key)
		if err != nil {
			return "", err
		}
		request.ImageLayers = append(request.ImageLayers, videoprocessing.ImageLayer{Path: path, StartTime: l.Start, EndTime: l.End, X: l.X, Y: l.Y, Width: l.Width, Opacity: l.Opacity})
	}
	request.TextLayers = veRenderText(snap.TextLayers)
	data, err := f.processor.Merge(ctx, request)
	if err != nil {
		return "", err
	}
	if len(data) > 512<<20 {
		return "", ErrVESize
	}
	key := fmt.Sprintf("video-editor/%s/%s/exports/%s/%s.mp4", e.UserID, e.ProjectID, e.ID, e.AttemptToken.String())
	if err = f.storage.Upload(ctx, key, data, "video/mp4"); err != nil {
		return "", ErrVEStorage
	}
	return key, nil
}
func veRenderText(layers []models.VideoEditorTextLayer) []videoprocessing.TextLayer {
	out := []videoprocessing.TextLayer{}
	for _, l := range layers {
		out = append(out, videoprocessing.TextLayer{Absolute: true, Text: l.Text, HasXY: true, EditorStyle: true, X: l.X, Y: l.Y, Align: l.Align, FontSize: l.FontSize, ScaleX: veTextScale(l.ScaleX), ScaleY: veTextScale(l.ScaleY), Color: strings.TrimPrefix(l.Color, "#"), StartTime: l.Start, EndTime: l.End, OutlineWidth: l.OutlineWidth, OutlineColor: strings.TrimPrefix(l.OutlineColor, "#"), BoxColor: strings.TrimPrefix(l.BoxColor, "#"), BoxOpacity: l.BoxOpacity, Animation: l.Animation})
	}
	return out
}

func veTextScale(value *float64) float64 {
	if value == nil {
		return 1
	}
	return *value
}
