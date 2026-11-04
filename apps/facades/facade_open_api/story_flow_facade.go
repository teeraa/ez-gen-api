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
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrStoryFlowUnavailable = errors.New("story flow unavailable")
var ErrStoryFlowConflict = errors.New("story flow conflict")

type StoryFlowValidationError struct {
	Field   string
	Message string
}

func (e *StoryFlowValidationError) Error() string { return e.Message }
func storyInvalid(field, msg string) error        { return &StoryFlowValidationError{field, msg} }

type storyFlowStore interface {
	Create(context.Context, *models.StoryFlowProject) (*models.StoryFlowProject, error)
	Get(context.Context, string, string) (*models.StoryFlowProject, error)
	ByKey(context.Context, string, string) (*models.StoryFlowProject, error)
	List(context.Context, string, int, int) ([]models.StoryFlowProject, int64, error)
	Change(context.Context, string, string, func(*models.StoryFlowProject) error) error
	Claim(context.Context) (*models.StoryFlowProject, error)
	Save(context.Context, *models.StoryFlowProject, bool) error
}
type storyFlowAssets interface {
	Asset(context.Context, string, string) (*models.LibraryAsset, error)
}
type storyFlowStorage interface {
	mediaStorage
	SignedDownloadURL(context.Context, string, time.Duration, string) (string, time.Time, error)
}
type StoryFlowOptions struct {
	OwnedMediaFetcher interface {
		Fetch(context.Context, string) ([]byte, string, error)
	}
	AnalysisModel   string
	ImageModel      string
	Enabled         bool
	ExportAvailable bool
}
type StoryFlowFacade struct {
	store     storyFlowStore
	assets    storyFlowAssets
	registry  *modelark.Registry
	images    modelark.ImageGenerator
	analyzer  modelark.StoryFlowAnalyzer
	video     modelark.Client
	storage   storyFlowStorage
	fetcher   mediaFetcher
	processor videoprocessing.VideoProcessor
	options   StoryFlowOptions
}

func NewStoryFlowFacade(s storyFlowStore, a storyFlowAssets, r *modelark.Registry, i modelark.ImageGenerator, n modelark.StoryFlowAnalyzer, v modelark.Client, storage storyFlowStorage, fetcher mediaFetcher, p videoprocessing.VideoProcessor, o StoryFlowOptions) *StoryFlowFacade {
	return &StoryFlowFacade{s, a, r, i, n, v, storage, fetcher, p, o}
}
func storyStyles() []string {
	return []string{"water_color", "pixel_art", "claymation", "anime", "low_poly", "realistic"}
}
func storyStyle(s string) string {
	switch s {
	case "water_color":
		return "Watercolor animation: translucent pigment washes, hand-painted paper texture and consistent watercolor rendering."
	case "pixel_art":
		return "Pixel-art animation: crisp intentional pixels, limited coherent palette, consistent pixel scale; no photographic rendering."
	case "claymation":
		return "Claymation stop-motion: tactile sculpted clay surfaces and coherent handmade forms."
	case "anime":
		return "Anime animation: consistent cel shading, clear line art and expressive anime character design."
	case "low_poly":
		return "Low-poly 3D animation: faceted geometry, simple coherent materials and consistent polygonal art direction."
	case "realistic":
		return "Photorealistic cinematic live action with natural human skin, hair, fabric and physically plausible photographed motion. Use character references only for broad face geometry, hairstyle, body proportions and wardrobe design. Replace CG surface rendering, materials and source lighting with natural filmed appearance and the saved shot lighting."
	}
	return ""
}
func storyQuality(m modelark.ModelConfig) []string {
	if !m.SupportsImgVideo || strings.Contains(m.ProviderModelID, "2-5") {
		return nil
	}
	q := []string{"standard"}
	if m.MaxResolution == "1080p" || m.MaxResolution == "4k" {
		q = append(q, "high")
	}
	return q
}
func (f *StoryFlowFacade) Capabilities() dto.StoryFlowCapabilitiesDTO {
	d := dto.StoryFlowCapabilitiesDTO{Enabled: f.options.Enabled && f.options.AnalysisModel != "" && f.options.ImageModel != "" && f.options.ExportAvailable, Models: []dto.StoryFlowModelDTO{}, Styles: storyStyles(), MaxScenes: 10, MaxShots: 20}
	ids := []string{}
	for id := range f.registry.Routes() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m, _ := f.registry.Lookup(id)
		q := storyQuality(m)
		if len(q) == 0 {
			continue
		}
		ratios := []string{}
		for _, r := range m.SupportedRatios {
			if ImageSize(r) != "" {
				ratios = append(ratios, r)
			}
		}
		if len(ratios) > 0 {
			d.Models = append(d.Models, dto.StoryFlowModelDTO{ID: id, Name: m.DisplayName, AspectRatios: ratios, Qualities: q, MinDuration: m.MinDuration, MaxDuration: m.MaxDuration})
		}
	}
	if len(d.Models) == 0 {
		d.Enabled = false
	}
	if !d.Enabled {
		d.UnavailableReason = "Story Flow ยังไม่พร้อม กรุณาตั้งค่าโมเดลวิเคราะห์ โมเดลภาพ วิดีโอ และ FFmpeg"
	}
	return d
}
func storyHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func storyBound(s string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return utf8.ValidString(s) && n >= min && n <= max
}
func (f *StoryFlowFacade) validate(in req.CreateStoryFlowRequest) error {
	for _, v := range []struct {
		field, value string
		min, max     int
	}{{"title", in.Title, 1, 120}, {"idea", in.Idea, 1, 6000}, {"newCharacterPrompt", in.NewCharacterPrompt, 0, 2000}} {
		if !storyBound(v.value, v.min, v.max) {
			return storyInvalid(v.field, "กรุณากรอกข้อมูลให้ครบและไม่เกินความยาวที่กำหนด")
		}
	}
	if storyStyle(in.Style) == "" {
		return storyInvalid("style", "กรุณาเลือกสไตล์")
	}
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return storyInvalid("idempotencyKey", "รหัสคำขอไม่ถูกต้อง")
	}
	if in.SceneCount < 1 || in.SceneCount > 10 {
		return storyInvalid("sceneCount", "เลือกจำนวนฉาก 1–10 ฉาก")
	}
	if len(in.MoodTags) > 5 {
		return storyInvalid("moodTags", "เลือกอารมณ์ได้สูงสุด 5 รายการ")
	}
	for _, s := range in.MoodTags {
		if !storyBound(s, 1, 40) {
			return storyInvalid("moodTags", "แท็กอารมณ์ต้องมี 1–40 ตัวอักษร")
		}
	}
	if len(in.CharacterAssetIDs) > 8 {
		return storyInvalid("characterAssetIds", "เลือกตัวละครได้สูงสุด 8 ตัว")
	}
	seen := map[string]bool{}
	for _, id := range in.CharacterAssetIDs {
		if _, e := uuid.Parse(id); e != nil || seen[id] {
			return storyInvalid("characterAssetIds", "รหัสตัวละครไม่ถูกต้องหรือซ้ำ")
		}
		seen[id] = true
	}
	m, ok := f.registry.Lookup(in.ModelID)
	if !ok || !m.SupportsOutput(in.AspectRatio, m.MinDuration) || ImageSize(in.AspectRatio) == "" {
		return storyInvalid("modelId", "โมเดลไม่รองรับการตั้งค่านี้")
	}
	valid := false
	for _, q := range storyQuality(m) {
		if q == in.Quality {
			valid = true
		}
	}
	if !valid {
		return storyInvalid("quality", "โมเดลไม่รองรับคุณภาพนี้")
	}
	return nil
}
func storyEncode(p *models.StoryFlowProject, s *dto.StoryFlowSnapshot) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	p.Document = b
	p.Status = s.Status
	return nil
}
func (f *StoryFlowFacade) Create(c context.Context, user string, in req.CreateStoryFlowRequest) (*dto.StoryFlowProjectDTO, error) {
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return nil, storyInvalid("idempotencyKey", "รหัสคำขอไม่ถูกต้อง")
	}
	if old, e := f.store.ByKey(c, in.IdempotencyKey, user); e == nil {
		if old.RequestHash != storyHash(in) {
			return nil, ErrIdempotencyMismatch
		}
		return f.view(c, old)
	} else if !errors.Is(e, repo.ErrNotFound) {
		return nil, e
	}
	if e := f.validate(in); e != nil {
		return nil, e
	}
	if !f.Capabilities().Enabled {
		return nil, ErrStoryFlowUnavailable
	}
	uid, e := uuid.Parse(user)
	if e != nil {
		return nil, ErrUnauthorized
	}
	m, _ := f.registry.Lookup(in.ModelID)
	s := dto.StoryFlowSnapshot{Input: in, Model: m, ImageModel: f.options.ImageModel, AnalysisModel: f.options.AnalysisModel, PromptVersion: "story-flow-v1", Revision: 1, Status: "draft", Stage: "draft", Scenes: []models.StoryFlowScene{}, Characters: []models.StoryFlowCharacter{}, Operations: map[string]string{}}
	for _, id := range in.CharacterAssetIDs {
		a, e := f.assets.Asset(c, id, user)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if a.AssetType != "character" || a.StorageKey == nil || *a.StorageKey == "" {
			return nil, storyInvalid("characterAssetIds", "ตัวละครยังไม่มีภาพพร้อมใช้งาน")
		}
		s.Characters = append(s.Characters, models.StoryFlowCharacter{ID: id, Name: a.Name, Description: a.Description + " " + a.Prompt, AppearanceExcerpt: storyExcerpt(a.Description+" "+a.Prompt, 160), StorageKey: *a.StorageKey})
	}
	if in.NewCharacterPrompt != "" {
		prompt := in.NewCharacterPrompt + "\n" + storyStyle(in.Style) + "\nOne single full-body fictional character reference. No panels, text or watermark. Plain neutral background."
		if in.Style == "realistic" {
			prompt, e = characterdesign.CompilePipelineStylized3D(in.NewCharacterPrompt, characterdesign.Options{VisualStyle: "cinematic_realistic", Mood: "neutral", DesignFormat: "full_body"}, false)
			if e != nil {
				return nil, e
			}
		}
		s.CharacterCompilerVersion = "story-flow-character-v1"
		if in.Style == "realistic" {
			s.CharacterCompilerVersion = characterdesign.PipelineStylized3DCompilerVersion
		}
		s.CharacterPrompt = prompt
		s.CharacterStatus = "pending"
		s.Characters = append(s.Characters, models.StoryFlowCharacter{ID: "new-character", Name: "ตัวละครใหม่", Description: in.NewCharacterPrompt, AppearanceExcerpt: storyExcerpt(in.NewCharacterPrompt, 160)})
	}
	p := &models.StoryFlowProject{UserID: uid, IdempotencyKey: in.IdempotencyKey, RequestHash: storyHash(in)}
	if e = storyEncode(p, &s); e != nil {
		return nil, e
	}
	p, e = f.store.Create(c, p)
	if e != nil {
		return nil, e
	}
	if p.RequestHash != storyHash(in) {
		return nil, ErrIdempotencyMismatch
	}
	return f.view(c, p)
}
func (f *StoryFlowFacade) Get(c context.Context, id, user string) (*dto.StoryFlowProjectDTO, error) {
	if _, e := uuid.Parse(id); e != nil {
		return nil, ErrNotFound
	}
	p, e := f.store.Get(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	return f.view(c, p)
}
func (f *StoryFlowFacade) List(c context.Context, user string, page, limit int) (*dto.StoryFlowListDTO, error) {
	rows, n, e := f.store.List(c, user, page, limit)
	if e != nil {
		return nil, e
	}
	d := &dto.StoryFlowListDTO{Items: []dto.StoryFlowProjectDTO{}, Total: n, Page: page, Limit: limit}
	for i := range rows {
		v, e := f.view(c, &rows[i])
		if e != nil {
			return nil, e
		}
		d.Items = append(d.Items, *v)
	}
	return d, nil
}
func (f *StoryFlowFacade) Action(c context.Context, id, user, action string, in req.StoryFlowActionRequest) (*dto.StoryFlowProjectDTO, error) {
	if _, e := uuid.Parse(id); e != nil {
		return nil, ErrNotFound
	}
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return nil, storyInvalid("idempotencyKey", "รหัสคำขอไม่ถูกต้อง")
	}
	e := f.store.Change(c, id, user, func(p *models.StoryFlowProject) error {
		s, e := dto.DecodeStoryFlow(p)
		if e != nil {
			return e
		}
		if s.Operations == nil {
			s.Operations = map[string]string{}
		}
		hash := storyHash(struct {
			Action string
			Input  req.StoryFlowActionRequest
		}{action, in})
		if old, ok := s.Operations[in.IdempotencyKey]; ok {
			if old != hash {
				return ErrIdempotencyMismatch
			}
			return nil
		}
		if in.Revision != s.Revision || p.LeaseToken != nil || s.Status == "queued" || s.Status == "processing" {
			return ErrStoryFlowConflict
		}
		switch action {
		case "story":
			if s.Stage != "draft" {
				return ErrStoryFlowConflict
			}
			s.Stage = "story"
		case "storyboard":
			if s.Stage != "story_review" {
				return ErrStoryFlowConflict
			}
			if len(in.SceneShots) != len(s.Scenes) {
				return storyInvalid("sceneShots", "กำหนดจำนวน Shot ให้ครบทุกฉาก")
			}
			total := 0
			for i, v := range in.SceneShots {
				if v.SceneIndex != i+1 || v.ShotCount < 1 || v.ShotCount > 20 {
					return storyInvalid("sceneShots", "ลำดับหรือจำนวน Shot ไม่ถูกต้อง")
				}
				total += v.ShotCount
			}
			if total > 20 {
				return storyInvalid("sceneShots", "จำนวน Shot รวมต้องไม่เกิน 20")
			}
			s.SceneShots = in.SceneShots
			s.Stage = "shots"
		case "video":
			if s.Stage != "storyboard_review" {
				return ErrStoryFlowConflict
			}
			for _, sc := range s.Scenes {
				for _, sh := range sc.Shots {
					if sh.ImageStatus != "succeeded" || sh.ImageKey == "" {
						return ErrStoryFlowConflict
					}
				}
			}
			s.Stage = "video"
		case "retry":
			if s.Status != "failed" || in.Stage != s.Stage {
				return ErrStoryFlowConflict
			}
			if in.ShotID != "" {
				found := false
				for i := range s.Scenes {
					for j := range s.Scenes[i].Shots {
						sh := &s.Scenes[i].Shots[j]
						if sh.ID == in.ShotID && sh.CanRetry {
							if s.Stage == "storyboard" {
								sh.ImageStatus = "pending"
							} else if s.Stage == "video" {
								if sh.ProviderTaskID != "" {
									sh.VideoStatus = "processing"
								} else {
									sh.VideoStatus = "pending"
								}
							} else {
								return ErrStoryFlowConflict
							}
							sh.CanRetry = false
							sh.FailureCode = ""
							sh.FailureMessage = ""
							found = true
						}
					}
				}
				if !found {
					return ErrStoryFlowConflict
				}
			} else {
				if !s.CanRetry {
					return ErrStoryFlowConflict
				}
				if s.Stage == "story" && s.CharacterStatus == "failed" {
					s.CharacterStatus = "pending"
				}
			}
		default:
			return ErrStoryFlowConflict
		}
		s.Operations[in.IdempotencyKey] = hash
		s.Status = "queued"
		s.CanRetry = false
		s.FailureCode = ""
		s.FailureMessage = ""
		return storyEncode(p, &s)
	})
	if e != nil {
		if errors.Is(e, repo.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, e
	}
	return f.Get(c, id, user)
}
func (f *StoryFlowFacade) view(c context.Context, p *models.StoryFlowProject) (*dto.StoryFlowProjectDTO, error) {
	s, e := dto.DecodeStoryFlow(p)
	if e != nil {
		return nil, e
	}
	v := &dto.StoryFlowProjectDTO{ID: p.ID.String(), CreateStoryFlowRequest: s.Input, Revision: s.Revision, Status: s.Status, Stage: s.Stage, Scenes: []dto.StoryFlowSceneDTO{}, FailureCode: s.FailureCode, FailureMessage: s.FailureMessage, CanRetry: s.CanRetry, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339)}
	v.CharacterSummaries = []dto.StoryFlowCharacterSummary{}
	for _, ch := range s.Characters {
		v.CharacterSummaries = append(v.CharacterSummaries, dto.StoryFlowCharacterSummary{ID: ch.ID, Name: ch.Name})
	}
	sign := func(key string) *string {
		if key == "" {
			return nil
		}
		u, _, e := f.storage.SignedURL(c, key, time.Hour)
		if e != nil {
			return nil
		}
		return &u
	}
	for _, sc := range s.Scenes {
		d := dto.StoryFlowSceneDTO{SceneIndex: sc.SceneIndex, Title: sc.Title, Narrative: sc.Narrative, Location: sc.Location, Characters: sc.Characters, Emotion: sc.Emotion, Continuity: sc.Continuity, Shots: []dto.StoryFlowShotDTO{}}
		for _, sh := range sc.Shots {
			v.TotalShots++
			if sh.ImageStatus == "succeeded" {
				v.CompletedImages++
			}
			if sh.VideoStatus == "succeeded" {
				v.CompletedVideos++
			}
			d.Shots = append(d.Shots, dto.StoryFlowShotDTO{ID: sh.ID, SceneIndex: sh.SceneIndex, ShotIndex: sh.ShotIndex, Action: sh.Action, Characters: sh.Characters, Location: sh.Location, Atmosphere: sh.Atmosphere, Framing: sh.Framing, CameraAngle: sh.CameraAngle, CameraMovement: sh.CameraMovement, Continuity: sh.Continuity, ImagePrompt: sh.ImagePrompt, VideoPrompt: sh.VideoPrompt, DurationSeconds: sh.DurationSeconds, ImageStatus: sh.ImageStatus, VideoStatus: sh.VideoStatus, ImageURL: sign(sh.ImageKey), VideoURL: sign(sh.VideoKey), FailureCode: sh.FailureCode, FailureMessage: sh.FailureMessage, CanRetry: sh.CanRetry})
		}
		v.Scenes = append(v.Scenes, d)
	}
	v.OutputURL = sign(s.OutputKey)
	if s.OutputKey != "" {
		u, _, e := f.storage.SignedDownloadURL(c, s.OutputKey, time.Hour, "story-flow.mp4")
		if e == nil {
			v.DownloadURL = &u
		}
	}
	return v, nil
}
func storyFailure(s *dto.StoryFlowSnapshot, code, msg string, retry bool) {
	s.Status = "failed"
	s.FailureCode = code
	s.FailureMessage = msg
	s.CanRetry = retry
}
func storyAttention(s *dto.StoryFlowSnapshot) {
	s.Status = "needs_attention"
	s.FailureCode = "SUBMISSION_UNCERTAIN"
	s.FailureMessage = "ยืนยันผลการส่งงานไม่ได้ กรุณาติดต่อผู้ดูแลระบบก่อนส่งซ้ำ"
	s.CanRetry = false
}
func storySnapshotPrompt(s *dto.StoryFlowSnapshot, stage string) string {
	data, _ := json.Marshal(struct {
		Project     req.CreateStoryFlowRequest  `json:"project"`
		Characters  []models.StoryFlowCharacter `json:"characters"`
		Scenes      []models.StoryFlowScene     `json:"scenes,omitempty"`
		SceneShots  []req.StoryFlowSceneShots   `json:"sceneShots,omitempty"`
		MinDuration int                         `json:"minDuration"`
		MaxDuration int                         `json:"maxDuration"`
	}{s.Input, storyCharacterDescriptions(s.Characters), s.Scenes, s.SceneShots, s.Model.MinDuration, s.Model.MaxDuration})
	schema := `Return {"scenes":[{"sceneIndex":1,"title":"...","narrative":"...","location":"...","characters":["supplied-character-id"],"emotion":"...","continuity":{"wardrobe":"...","lighting":"...","characterState":"..."},"shots":[]}]} with exactly project.sceneCount scenes in ascending contiguous 1-based order. Each field is required. Keep continuity fields at most120 characters. Characters must be supplied IDs. Never invent character IDs; use [] when none appear. Every text field must contain concise nonempty text, at most2000 characters. Include outfits, lighting and character state across scenes.`
	if stage == "shots" {
		schema = `Return {"scenes":[{"sceneIndex":1,"shots":[{"sceneIndex":1,"shotIndex":1,"action":"...","characters":[],"location":"...","atmosphere":"...","framing":"...","cameraAngle":"...","cameraMovement":"...","continuity":{"wardrobe":"...","lighting":"...","characterState":"..."},"imagePrompt":"...","videoPrompt":"...","durationSeconds":5}]}]}. Return exactly the supplied number of scenes and exact sceneShots shotCount for each scene. All indices contiguous 1-based. Preserve saved scenes without rewriting. Include all required shot fields, nonempty concise strings. Keep each action/location/camera/continuity field at most120 characters, and each image/video prompt at most350 characters. Leave room for server-applied style/continuity directions within the final2000-character provider prompt budget. Characters only supplied IDs. Duration integer within supplied model minDuration/maxDuration and suitable for action. Exactly one image per shot, never contact sheet. No voiceover/music. Plan visual action only. Do not invent spoken dialogue; no generated audio is requested in this version.`
	}
	return schema + "\nVisual direction: " + storyStyle(s.Input.Style) + "\nINPUT DATA:\n" + string(data)
}
func storyCharacterDescriptions(in []models.StoryFlowCharacter) []models.StoryFlowCharacter {
	out := make([]models.StoryFlowCharacter, len(in))
	copy(out, in)
	for i := range out {
		out[i].StorageKey = ""
	}
	return out
}
func storyPromptForShot(s *dto.StoryFlowSnapshot, sh *models.StoryFlowShot) {
	c, _ := json.Marshal(sh.Continuity)
	appearing := []models.StoryFlowCharacter{}
	for _, id := range sh.Characters {
		for _, ch := range s.Characters {
			if ch.ID == id {
				appearing = append(appearing, ch)
			}
		}
	}
	design := make([]struct {
		ID         string `json:"id"`
		Appearance string `json:"appearance"`
	}, 0, len(appearing))
	for _, ch := range appearing {
		design = append(design, struct {
			ID         string `json:"id"`
			Appearance string `json:"appearance"`
		}{ch.ID, ch.AppearanceExcerpt})
	}
	chars, _ := json.Marshal(design)
	continuity := "\nSaved continuity: " + string(c) + "\nOnly listed appearing characters; do not introduce other characters. Character design data: " + string(chars) + "\nSaved shot action: " + sh.Action + "\nLocation: " + sh.Location + "\nCamera: " + sh.Framing + ", " + sh.CameraAngle + ", " + sh.CameraMovement
	sh.ImagePrompt = sh.ImagePrompt + "\n" + storyStyle(s.Input.Style) + continuity + "\nOne single composed shot image. No panels, grids, captions or watermarks."
	sh.VideoPrompt = sh.VideoPrompt + "\n" + storyStyle(s.Input.Style) + continuity + "\nAnimate only the saved shot. Preserve character and wardrobe continuity."
}
func storyShotFailure(s *dto.StoryFlowSnapshot, sh *models.StoryFlowShot, stage, code, msg string, retry bool) {
	if stage == "storyboard" {
		sh.ImageStatus = "failed"
	} else {
		sh.VideoStatus = "failed"
	}
	sh.FailureCode = code
	sh.FailureMessage = msg
	sh.CanRetry = retry
	storyFailure(s, code, msg, false)
}
func storyKey(p *models.StoryFlowProject, kind, id, ext string) string {
	return fmt.Sprintf("story-flow/%s/%s/r1/%s/%s.%s", p.UserID, p.ID, kind, id, ext)
}

func storyExcerpt(text string, max int) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}
