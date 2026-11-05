package facade_open_api

import (
	"context"
	"encoding/json"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type DirectorError struct {
	Status          int
	Code            string
	Message         string
	CurrentRevision *int
}

func (e *DirectorError) Error() string { return e.Message }
func directorError(status int, code string) error {
	return &DirectorError{Status: status, Code: code, Message: "ไม่สามารถดำเนินการได้ในสถานะนี้ กรุณาโหลดข้อมูลล่าสุดแล้วลองใหม่"}
}
func directorInvalid(field string) error {
	return &StoryFlowValidationError{Field: field, Message: "กรุณาตรวจข้อมูลในช่องนี้ให้ถูกต้องและไม่เกินขีดจำกัด"}
}
func directorPointer[T any](v T) *T { return &v }
func directorBusy(s string) bool    { return s == "queued" || s == "processing" || s == "pending" }

type directorStore interface {
	Create(context.Context, *models.DirectorProject, string, string) (*models.DirectorProject, error)
	Get(context.Context, string, string) (*models.DirectorProject, error)
	List(context.Context, string, int, int, string) ([]models.DirectorProject, int64, error)
	Change(context.Context, string, string, string, string, func(*models.DirectorProject, func(string) (*models.LibraryAsset, error)) ([]models.DirectorJob, error)) (*models.DirectorProject, error)
	Claim(context.Context) (*models.DirectorJob, error)
	Complete(context.Context, *models.DirectorJob, string, bool, func(*models.DirectorProject) error) error
	Progress(context.Context, *models.DirectorJob, func(*models.DirectorProject) error) error
	RecordOutput(context.Context, *models.DirectorJob, []byte) error
	Wait(context.Context, *models.DirectorJob, []byte, time.Time) error
	Balance(context.Context, string) (int64, error)
}
type DirectorOptions struct {
	Enabled       bool
	AnalysisModel string
	ImageModel    string
	StoryPoints   int
	ImagePoints   int
	VideoPoints   int
	DraftPoints   int
	// MaxImageReferences caps character images sent per Seedream request; other cast members are
	// described in text. 0 is text-only, matching the Character Studio descriptive strategy.
	MaxImageReferences int
	SampleBaseURL      string
	VideoEnabled       bool
	// ReferenceCheck confirms a signed reference URL is downloadable before it is sent to the
	// provider; nil skips the check (tests and storage without public signed URLs).
	ReferenceCheck func(context.Context, string) error
	// LookDescriber turns each character reference image into one fixed appearance text that
	// every shot reuses, so outfits stay identical even for characters sent without an image.
	LookDescriber DirectorLookDescriber
	ImageFetch    func(context.Context, string) ([]byte, string, error)
	// Processor and VideoFetch merge finished shot clips into one exported MP4.
	Processor  videoprocessing.VideoProcessor
	VideoFetch func(context.Context, string) ([]byte, string, error)
}

type DirectorLookDescriber interface {
	DescribeImage(ctx context.Context, model string, image []byte, mime string) (string, error)
}
type DirectorFacade struct {
	store    directorStore
	assets   storyFlowAssets
	storage  mediaStorage
	analyzer modelark.StoryFlowAnalyzer
	images   modelark.ImageGenerator
	registry *modelark.Registry
	video    modelark.Client
	fetcher  mediaFetcher
	options  DirectorOptions
}

// A nil video client or fetcher leaves the storyboard usable and reports video as unavailable.
func NewDirectorFacade(s directorStore, a storyFlowAssets, st mediaStorage, n modelark.StoryFlowAnalyzer, i modelark.ImageGenerator, r *modelark.Registry, v modelark.Client, fe mediaFetcher, o DirectorOptions) *DirectorFacade {
	return &DirectorFacade{store: s, assets: a, storage: st, analyzer: n, images: i, registry: r, video: v, fetcher: fe, options: o}
}

// Director offers explicit Seedance generations. They bypass the registry's ARK_DEFAULT_MODEL
// override, which otherwise maps every video menu entry to one provider model.
var directorVideoModels = []struct{ ID, Provider string }{
	{"seedance-2-0-fast", "dreamina-seedance-2-0-fast-260128"},
	{"seedance-2-5", "dreamina-seedance-2-5-260628"},
	{"seedance-2-0", "dreamina-seedance-2-0-260128"},
}

// Projects created before explicit model IDs stored video menu IDs.
var directorLegacyVideoModels = map[string]string{"violet-motion-2": "seedance-2-0-fast", "violet-fast": "seedance-2-0-fast", "violet-cinema": "seedance-2-0", "violet-realism": "seedance-2-5", "violet-classic": "seedance-2-0-fast"}

func directorCanonicalVideoModel(id string) string {
	if next, ok := directorLegacyVideoModels[id]; ok {
		return next
	}
	return id
}
func directorVideoModel(id string) (modelark.ModelConfig, bool) {
	id = directorCanonicalVideoModel(id)
	for _, m := range directorVideoModels {
		if m.ID == id {
			return modelark.ProviderModel(m.Provider)
		}
	}
	return modelark.ModelConfig{}, false
}

// Display labels name the configured model generation so testers can see which model ran.
func directorModelLabel(model string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(model)), "-")
	if len(parts) == 0 || parts[0] == "" {
		return "มาตรฐาน"
	}
	isNumber := func(s string) bool { _, e := strconv.Atoi(s); return e == nil && len(s) <= 2 }
	switch parts[0] {
	case "seedream", "seedance":
		if len(parts) >= 3 && isNumber(parts[1]) && isNumber(parts[2]) {
			return strings.ToUpper(parts[0][:1]) + parts[0][1:] + " " + parts[1] + "." + parts[2]
		}
	case "gemini":
		words := []string{"Gemini"}
		for _, w := range parts[1:] {
			if w == "" || (len(w) >= 6 && isNumber(w[:2])) {
				continue
			}
			words = append(words, strings.ToUpper(w[:1])+w[1:])
		}
		return strings.Join(words, " ")
	}
	return model
}

const directorVideoSecondsTarget = 5

func directorVideoSeconds(m modelark.ModelConfig) int {
	return min(max(directorVideoSecondsTarget, m.MinDuration), m.MaxDuration)
}
func directorVideoResolution(project string, m modelark.ModelConfig) string {
	if m.MaxResolution == "720p" {
		return "720p"
	}
	return project
}
func (f *DirectorFacade) videoAvailable() bool {
	return f.options.VideoEnabled && f.video != nil && f.fetcher != nil
}

// The public response never includes this private aggregate metadata.
type directorDocument struct {
	Project     dto.DirectorProject `json:"project"`
	Keys        map[string]string   `json:"keys"`
	Generations map[string]string   `json:"generations"`
	// Planner-authored production context; private because it is English provider guidance.
	Bible    string                           `json:"bible,omitempty"`
	Settings map[string]string                `json:"settings,omitempty"`
	Prompts  map[string]directorPlannedPrompt `json:"prompts,omitempty"`
	// Provider draft handles stay private; they are only used to refine a draft into a final clip.
	VideoDrafts map[string]directorVideoDraft `json:"videoDrafts,omitempty"`
	// Looks caches the described appearance per character asset, keyed by the image it describes.
	Looks map[string]directorLook `json:"looks,omitempty"`
	// ExportSource hashes the clip keys the current export was built from.
	ExportSource string `json:"exportSource,omitempty"`
}
type directorLook struct {
	Text string `json:"text"`
	Key  string `json:"key"`
}
type directorVideoDraft struct {
	TaskID    string    `json:"taskId"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"createdAt"`
}
type directorJobInput struct {
	Project      dto.DirectorProject `json:"project"`
	Generation   string              `json:"generation"`
	Description  string              `json:"description"`
	Camera       string              `json:"camera"`
	Instruction  string              `json:"instruction"`
	Model        string              `json:"model"`
	ImageModel   string              `json:"imageModel"`
	Characters   []directorCharacter `json:"characters"`
	CoverKey     string              `json:"coverKey"`
	ShotID       string              `json:"shotId,omitempty"`
	ImageKey     string              `json:"imageKey,omitempty"`
	VideoModel   string              `json:"videoModel,omitempty"`
	Ratio        string              `json:"ratio,omitempty"`
	Resolution   string              `json:"resolution,omitempty"`
	Seconds      int                 `json:"seconds,omitempty"`
	NextShotID   string              `json:"nextShotId,omitempty"`
	NextImageKey string              `json:"nextImageKey,omitempty"`
	Draft        bool                `json:"draft,omitempty"`
	DraftTaskID  string              `json:"draftTaskId,omitempty"`
	ClipKeys     []string            `json:"clipKeys,omitempty"`
}
type directorCharacter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Key         string `json:"key"`
}

func directorDecode(p *models.DirectorProject) (directorDocument, error) {
	var d directorDocument
	e := json.Unmarshal(p.Document, &d)
	if d.Settings == nil {
		d.Settings = map[string]string{}
	}
	if d.Prompts == nil {
		d.Prompts = map[string]directorPlannedPrompt{}
	}
	if d.VideoDrafts == nil {
		d.VideoDrafts = map[string]directorVideoDraft{}
	}
	if d.Looks == nil {
		d.Looks = map[string]directorLook{}
	}
	return d, e
}
func directorEncode(p *models.DirectorProject, d *directorDocument) error {
	p.Name = d.Project.Name
	d.Project.UpdatedAt = time.Now().UTC()
	b, e := json.Marshal(d)
	p.Document = b
	return e
}
func (f *DirectorFacade) Capabilities() dto.DirectorCapabilities {
	c := dto.DirectorCapabilities{Enabled: f.options.Enabled && f.options.AnalysisModel != "" && f.options.ImageModel != "" && f.options.StoryPoints > 0 && f.options.ImagePoints > 0 && f.options.VideoPoints >= 0, AspectRatios: []dto.DirectorOption{}, Resolutions: []dto.DirectorResolution{}, Styles: []dto.DirectorStyle{}, Genres: []dto.DirectorOption{}, Moods: []string{"อบอุ่น", "ตื่นเต้น", "ลึกลับ", "มีความหวัง", "เศร้า", "ฮึกเหิม", "ตลกขบขัน", "น่ากลัว", "สงบ", "โรแมนติก"}, Cameras: []string{"มุมกว้าง", "มุมกลาง", "โคลสอัพ", "มุมสูง", "มุมต่ำ", "มุมข้ามไหล่"}, Limits: dto.DirectorLimits{IdeaMaxLength: 1000, NameMaxLength: 80, MinScenes: 1, MaxScenes: 10, MinShotsPerScene: 1, MaxShotsPerScene: 6, MaxShotsPerSceneAfterEdit: 8, MaxTotalShots: 40, MaxCast: 4, MaxMoods: 3, SceneSummaryMaxLength: 1200, ShotDescriptionMaxLength: 600}, Models: dto.DirectorModels{Story: []dto.DirectorModel{}, Image: []dto.DirectorModel{}, Video: []dto.DirectorModel{}}}
	for _, s := range []string{"16:9", "9:16", "1:1", "4:5"} {
		c.AspectRatios = append(c.AspectRatios, dto.DirectorOption{Value: s, Label: s, Hint: "สัดส่วนภาพ"})
	}
	for _, s := range []string{"720p", "1080p", "4k"} {
		c.Resolutions = append(c.Resolutions, dto.DirectorResolution{DirectorOption: dto.DirectorOption{Value: s, Label: s, Hint: "ความละเอียดวิดีโอในระยะถัดไป"}, Available: s != "4k"})
	}
	names := []string{"ซีเนมาติก", "อนิเมะ", "3D การ์ตูน", "สีน้ำ", "ดินปั้น", "สมจริง"}
	for i, s := range []string{"cinematic", "anime", "3d", "watercolor", "clay", "realistic"} {
		sample := ""
		if f.options.SampleBaseURL != "" {
			sample = strings.TrimRight(f.options.SampleBaseURL, "/") + "/" + s + ".jpg"
		}
		c.Styles = append(c.Styles, dto.DirectorStyle{DirectorOption: dto.DirectorOption{Value: s, Label: names[i], Hint: names[i]}, SampleImageURL: sample})
	}
	labels := []string{"ผจญภัย", "ดราม่า", "แฟนตาซี", "ตลก", "ระทึกขวัญ", "ไซไฟ", "โรแมนติก", "โฆษณา"}
	for i, s := range []string{"adventure", "drama", "fantasy", "comedy", "thriller", "scifi", "romance", "ad"} {
		c.Genres = append(c.Genres, dto.DirectorOption{Value: s, Label: labels[i], Hint: labels[i]})
	}
	if f.options.AnalysisModel != "" {
		c.Models.Story = append(c.Models.Story, dto.DirectorModel{ID: "director-story", Name: directorModelLabel(f.options.AnalysisModel), Note: "เขียนเรื่องและวางแผนฉาก · " + f.options.AnalysisModel, PointsPerUnit: f.options.StoryPoints, Default: true})
	}
	if f.options.ImageModel != "" {
		c.Models.Image = append(c.Models.Image, dto.DirectorModel{ID: "director-image", Name: directorModelLabel(f.options.ImageModel), Note: "สร้างภาพจากคำบรรยายและตัวละคร · " + f.options.ImageModel, PointsPerUnit: f.options.ImagePoints, SupportsCharacterReference: true, Default: true})
	}
	for _, entry := range directorVideoModels {
		m, ok := modelark.ProviderModel(entry.Provider)
		if !ok || !m.SupportsImgVideo {
			continue
		}
		model := dto.DirectorModel{ID: entry.ID, Name: m.PublicLabel(), Note: fmt.Sprintf("ภาพ shot นี้เป็นเฟรมแรก ภาพ shot ถัดไปเป็นเฟรมสุดท้าย · สูงสุด %s", m.MaxResolution), SpeedHint: fmt.Sprintf("%d วินาทีต่อ shot", directorVideoSeconds(m)), PointsPerUnit: f.options.VideoPoints, Default: len(c.Models.Video) == 0, AspectRatios: append([]string{}, m.SupportedRatios...)}
		if m.SupportsDraft() && f.options.DraftPoints > 0 {
			model.SupportsDraft = true
			model.DraftPointsPerUnit = f.options.DraftPoints
			model.Note += " · มีโหมด draft 480p"
		}
		c.Models.Video = append(c.Models.Video, model)
	}
	c.VideoEnabled = c.Enabled && f.videoAvailable() && len(c.Models.Video) > 0
	if !c.Enabled {
		missing := []string{}
		if !f.options.Enabled || f.options.AnalysisModel == "" {
			missing = append(missing, "โมเดลเขียนเนื้อเรื่อง")
		}
		if f.options.ImageModel == "" {
			missing = append(missing, "โมเดลสร้างภาพ")
		}
		if f.options.StoryPoints <= 0 || f.options.ImagePoints <= 0 || f.options.VideoPoints < 0 {
			missing = append(missing, "ราคาพอยต์")
		}
		c.UnavailableReason = directorPointer("ผู้กำกับยังไม่พร้อมใช้งาน ยังไม่ได้ตั้งค่า: " + strings.Join(missing, ", ") + " กรุณาติดต่อผู้ดูแลระบบ")
	}
	return c
}
func directorBound(s string, min, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(strings.TrimSpace(s)) >= min && utf8.RuneCountInString(s) <= max
}
func directorContains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func directorOption(a []dto.DirectorOption, s string) bool {
	for _, x := range a {
		if x.Value == s {
			return true
		}
	}
	return false
}
func directorModel(a []dto.DirectorModel, s string) bool {
	for _, x := range a {
		if x.ID == s {
			return true
		}
	}
	return false
}
func (f *DirectorFacade) settings(p *dto.DirectorProject) error {
	c := f.Capabilities()
	if !directorBound(p.Name, 1, 80) {
		return directorInvalid("name")
	}
	if !directorOption(c.AspectRatios, p.AspectRatio) {
		return directorInvalid("aspectRatio")
	}
	style := false
	for _, s := range c.Styles {
		style = style || s.Value == p.Style
	}
	if !style {
		return directorInvalid("style")
	}
	valid := false
	for _, r := range c.Resolutions {
		valid = valid || (r.Value == p.Resolution && r.Available)
	}
	if !valid {
		return directorInvalid("resolution")
	}
	return nil
}
func (f *DirectorFacade) validateIdea(in dto.DirectorIdea, generate bool, loadAsset func(string) (*models.LibraryAsset, error)) error {
	cap := f.Capabilities()
	min := 0
	if generate {
		min = 10
	}
	if !directorBound(in.Prompt, min, 1000) {
		return directorInvalid("prompt")
	}
	if !directorOption(cap.Genres, in.Genre) {
		return directorInvalid("genre")
	}
	if in.SceneCount < 1 || in.SceneCount > 10 {
		return directorInvalid("sceneCount")
	}
	if len(in.Moods) > 3 || (generate && len(in.Moods) == 0) {
		return directorInvalid("moods")
	}
	seen := map[string]bool{}
	for _, m := range in.Moods {
		if seen[m] || !directorContains(cap.Moods, m) {
			return directorInvalid("moods")
		}
		seen[m] = true
	}
	if !directorModel(cap.Models.Story, in.StoryModelID) {
		return directorInvalid("storyModelId")
	}
	if len(in.CharacterAssetIDs) > 4 {
		return directorInvalid("characterAssetIds")
	}
	seen = map[string]bool{}
	for _, id := range in.CharacterAssetIDs {
		if _, e := uuid.Parse(id); e != nil || seen[id] {
			return directorInvalid("characterAssetIds")
		}
		seen[id] = true
		a, e := loadAsset(id)
		if e != nil || a.AssetType != "character" || a.StorageKey == nil {
			return directorInvalid("characterAssetIds")
		}
	}
	return nil
}
func (f *DirectorFacade) estimates(p *dto.DirectorProject) {
	n := 0
	for _, s := range p.Scenes {
		n += s.ShotCount
	}
	if len(p.Scenes) == 0 {
		n = p.Idea.SceneCount * 3
	}
	p.Estimates = dto.DirectorEstimates{Story: f.options.StoryPoints, Cover: f.options.ImagePoints, Storyboard: n * f.options.ImagePoints, Video: directorTotal(p) * f.options.VideoPoints}
}
func (f *DirectorFacade) present(c context.Context, m *models.DirectorProject) (dto.DirectorProject, error) {
	d, e := directorDecode(m)
	if e != nil {
		return dto.DirectorProject{}, e
	}
	p := d.Project
	p.Storyboard.VideoModelID = directorCanonicalVideoModel(p.Storyboard.VideoModelID)
	f.estimates(&p)
	sign := func(key string) (*string, *time.Time, error) {
		if key == "" {
			return nil, nil, nil
		}
		u, t, e := f.storage.SignedURL(c, key, time.Hour)
		if e != nil {
			return nil, nil, e
		}
		return &u, &t, nil
	}
	if p.Cover != nil {
		p.Cover.ImageURL, p.Cover.ExpiresAt, e = sign(d.Keys[p.Cover.ID])
		if e != nil {
			return p, e
		}
	}
	// Copy before filling display-only values so the stored document is not mutated.
	// Non-nil even when empty: clients require "scenes": [] rather than null.
	p.Scenes = append(make([]dto.DirectorScene, 0, len(p.Scenes)), p.Scenes...)
	var first *dto.DirectorShot
	for i := range p.Scenes {
		if p.Scenes[i].Setting == "" {
			p.Scenes[i].Setting = d.Settings[p.Scenes[i].ID]
		}
		for j := range p.Scenes[i].Shots {
			s := &p.Scenes[i].Shots[j]
			s.Image.URL, s.Image.ExpiresAt, e = sign(d.Keys[s.ID])
			if e != nil {
				return p, e
			}
			if s.Video != nil {
				s.Video.URL, s.Video.ExpiresAt, e = sign(d.Keys[directorVideoTarget(s.ID)])
				if e != nil {
					return p, e
				}
				draft, ok := d.VideoDrafts[s.ID]
				s.Video.CanFinalize = s.Video.Draft && s.Video.Status == "succeeded" && !s.Video.Stale && ok && time.Now().Before(modelark.DraftExpiresAt(draft.CreatedAt))
			}
			if first == nil && s.Image.Status == "succeeded" {
				first = s
			}
		}
	}
	if p.Export != nil {
		export := *p.Export
		export.URL, export.ExpiresAt, e = sign(d.Keys["export"])
		if e != nil {
			return p, e
		}
		export.Stale = export.Status == "succeeded" && d.ExportSource != storyHash(directorClipKeys(&d))
		p.Export = &export
	}
	p.CoverImageURL = nil
	p.CoverImageExpiresAt = nil
	if p.Cover != nil && p.Cover.Status == "succeeded" {
		p.CoverImageURL = p.Cover.ImageURL
		p.CoverImageExpiresAt = p.Cover.ExpiresAt
	} else if first != nil {
		p.CoverImageURL = first.Image.URL
		p.CoverImageExpiresAt = first.Image.ExpiresAt
	}
	return p, nil
}
func (f *DirectorFacade) Get(c context.Context, id, user string) (dto.DirectorProject, string, error) {
	if _, e := uuid.Parse(id); e != nil {
		return dto.DirectorProject{}, "", repo.ErrNotFound
	}
	m, e := f.store.Get(c, id, user)
	if e != nil {
		return dto.DirectorProject{}, "", e
	}
	p, e := f.present(c, m)
	return p, fmt.Sprintf("W/\"%d-%d\"", p.Revision, m.WorkVersion), e
}
func (f *DirectorFacade) List(c context.Context, user string, page, limit int, search string) (dto.DirectorList, error) {
	out := dto.DirectorList{Items: []dto.DirectorSummary{}, Page: page, Limit: limit}
	if page < 1 || limit < 1 || limit > 50 || !directorBound(search, 0, 80) {
		return out, directorInvalid("pagination")
	}
	rows, n, e := f.store.List(c, user, page, limit, search)
	out.Total = n
	if e != nil {
		return out, e
	}
	for i := range rows {
		p, e := f.present(c, &rows[i])
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, dto.DirectorSummary{ID: p.ID, Name: p.Name, AspectRatio: p.AspectRatio, Style: p.Style, Resolution: p.Resolution, Step: p.Step, StoryStatus: p.Story.Status, StoryboardStatus: p.Storyboard.Status, SceneCount: p.Idea.SceneCount, TotalShots: p.Storyboard.TotalShots, CompletedImages: p.Storyboard.CompletedImages, CoverImageURL: p.CoverImageURL, CoverImageExpiresAt: p.CoverImageExpiresAt, UpdatedAt: p.UpdatedAt})
	}
	return out, nil
}
func (f *DirectorFacade) Create(c context.Context, user string, in req.DirectorRequest) (dto.DirectorProject, error) {
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return dto.DirectorProject{}, directorInvalid("idempotencyKey")
	}
	if in.Name == nil || in.AspectRatio == nil || in.Style == nil || in.Resolution == nil {
		return dto.DirectorProject{}, directorInvalid("request")
	}
	now := time.Now().UTC()
	p := dto.DirectorProject{ID: uuid.NewString(), Revision: 1, Name: *in.Name, AspectRatio: *in.AspectRatio, Style: *in.Style, Resolution: *in.Resolution, Step: "idea", CreatedAt: now, UpdatedAt: now, Scenes: []dto.DirectorScene{}, Idea: dto.DirectorIdea{Genre: "adventure", SceneCount: 3, Moods: []string{}, CharacterAssetIDs: []string{}, StoryModelID: "director-story"}, Story: dto.DirectorStory{Status: "idle"}, Storyboard: dto.DirectorStoryboard{Status: "idle", ImageModelID: "director-image", StyleReference: "style"}}
	cap := f.Capabilities()
	if len(cap.Models.Video) > 0 {
		p.Storyboard.VideoModelID = cap.Models.Video[0].ID
	}
	if e := f.settings(&p); e != nil {
		return p, e
	}
	id, e := uuid.Parse(user)
	if e != nil {
		return p, repo.ErrNotFound
	}
	m := models.DirectorProject{Base: models.Base{ID: uuid.MustParse(p.ID), CreatedAt: now, UpdatedAt: now}, UserID: id, WorkVersion: 1}
	d := directorDocument{Project: p, Keys: map[string]string{}, Generations: map[string]string{}}
	if e = directorEncode(&m, &d); e != nil {
		return p, e
	}
	row, e := f.store.Create(c, &m, in.IdempotencyKey, storyHash(struct {
		Action string
		Input  req.DirectorRequest
	}{"create", in}))
	if e != nil {
		return p, e
	}
	return f.present(c, row)
}
func directorFindScene(p *dto.DirectorProject, id string) *dto.DirectorScene {
	for i := range p.Scenes {
		if p.Scenes[i].ID == id {
			return &p.Scenes[i]
		}
	}
	return nil
}
func directorFindShot(p *dto.DirectorProject, id string) *dto.DirectorShot {
	for i := range p.Scenes {
		for j := range p.Scenes[i].Shots {
			if p.Scenes[i].Shots[j].ID == id {
				return &p.Scenes[i].Shots[j]
			}
		}
	}
	return nil
}
func directorAnyWriting(p *dto.DirectorProject) bool {
	for _, s := range p.Scenes {
		if s.Status == "writing" {
			return true
		}
	}
	return false
}
func directorTotal(p *dto.DirectorProject) int {
	n := 0
	for _, s := range p.Scenes {
		n += len(s.Shots)
	}
	return n
}
func directorVideoTarget(shotID string) string { return "video:" + shotID }

// directorClipKeys lists every shot's finished clip in story order, or nil when any shot has
// no finished clip (an export must not silently skip story beats).
func directorClipKeys(d *directorDocument) []string {
	keys := []string{}
	for _, sc := range d.Project.Scenes {
		for _, sh := range sc.Shots {
			key := d.Keys[directorVideoTarget(sh.ID)]
			if sh.Video == nil || sh.Video.Status != "succeeded" || key == "" {
				return nil
			}
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// Continuity frames stay inside a scene; morphing into another location's first shot reads as a glitch.
func directorNextShot(p *dto.DirectorProject, sh *dto.DirectorShot) *dto.DirectorShot {
	sc := directorFindScene(p, sh.SceneID)
	if sc == nil {
		return nil
	}
	for i := range sc.Shots {
		if sc.Shots[i].ID == sh.ID && i+1 < len(sc.Shots) {
			return &sc.Shots[i+1]
		}
	}
	return nil
}
func directorPreviousShot(p *dto.DirectorProject, sh *dto.DirectorShot) *dto.DirectorShot {
	sc := directorFindScene(p, sh.SceneID)
	if sc == nil {
		return nil
	}
	for i := range sc.Shots {
		if sc.Shots[i].ID == sh.ID && i > 0 {
			return &sc.Shots[i-1]
		}
	}
	return nil
}
func directorVideoBusy(s *dto.DirectorShot) bool {
	return s.Video != nil && directorBusy(s.Video.Status)
}
func directorAnyVideoBusy(p *dto.DirectorProject) bool {
	for i := range p.Scenes {
		for j := range p.Scenes[i].Shots {
			if directorVideoBusy(&p.Scenes[i].Shots[j]) {
				return true
			}
		}
	}
	return false
}
func directorMarkVideoStale(s *dto.DirectorShot) {
	if s.Video != nil && s.Video.Status == "succeeded" {
		s.Video.Stale = true
	}
}
func directorRecount(p *dto.DirectorProject) {
	n, done, active, bad := 0, 0, false, false
	for _, sc := range p.Scenes {
		for _, s := range sc.Shots {
			n++
			if s.Image.Status == "succeeded" {
				done++
			}
			active = active || directorBusy(s.Image.Status)
			bad = bad || s.Image.Status == "failed" || s.Image.Status == "needs_attention"
		}
	}
	p.Storyboard.TotalShots = n
	p.Storyboard.CompletedImages = done
	if n > 0 {
		p.Storyboard.Status = "succeeded"
		if bad {
			p.Storyboard.Status = "needs_attention"
		}
		if active {
			p.Storyboard.Status = "processing"
		}
	}
}
func (f *DirectorFacade) job(d *directorDocument, kind, target string, cost int, in req.DirectorRequest, loadAsset func(string) (*models.LibraryAsset, error)) (models.DirectorJob, error) {
	snapshot := directorJobInput{Project: d.Project, Generation: uuid.NewString(), Model: f.options.ImageModel, ImageModel: f.options.ImageModel}
	if kind == "story" || kind == "rewrite" || kind == "plan" || kind == "add" {
		snapshot.Model = f.options.AnalysisModel
	}
	if in.Instruction != nil {
		snapshot.Instruction = *in.Instruction
	}
	if sh := directorFindShot(&d.Project, target); sh != nil {
		snapshot.Description = sh.Description
		snapshot.Camera = sh.Camera
	}
	if kind == "export" {
		snapshot.ClipKeys = directorClipKeys(d)
	}
	if kind == "video" {
		sh := directorFindShot(&d.Project, strings.TrimPrefix(target, "video:"))
		m, ok := directorVideoModel(d.Project.Storyboard.VideoModelID)
		if sh == nil || !ok {
			return models.DirectorJob{}, directorInvalid("shotIds")
		}
		snapshot.ShotID = sh.ID
		snapshot.Description = sh.Description
		snapshot.Camera = sh.Camera
		snapshot.ImageKey = d.Keys[sh.ID]
		snapshot.VideoModel = m.ProviderModelID
		snapshot.Ratio = d.Project.AspectRatio
		snapshot.Resolution = directorVideoResolution(d.Project.Resolution, m)
		snapshot.Seconds = directorVideoSeconds(m)
		if in.Finalize {
			draft := d.VideoDrafts[sh.ID]
			snapshot.DraftTaskID = draft.TaskID
			snapshot.VideoModel = draft.Provider
		} else {
			snapshot.Draft = in.Draft
			if next := directorNextShot(&d.Project, sh); next != nil && next.Image.Status == "succeeded" && d.Keys[next.ID] != "" {
				snapshot.NextShotID = next.ID
				snapshot.NextImageKey = d.Keys[next.ID]
			}
		}
	}
	for _, id := range d.Project.Idea.CharacterAssetIDs {
		a, e := loadAsset(id)
		if e != nil || a.StorageKey == nil {
			return models.DirectorJob{}, directorInvalid("characterAssetIds")
		}
		description := strings.TrimSpace(a.Description)
		if p := strings.TrimSpace(a.Prompt); p != "" && !strings.Contains(description, p) {
			description = strings.TrimSpace(description + "; " + p)
		}
		snapshot.Characters = append(snapshot.Characters, directorCharacter{ID: id, Name: a.Name, Description: description, Key: *a.StorageKey})
	}
	directorUniqueNames(snapshot.Characters)
	if d.Project.Cover != nil && d.Project.Cover.Source == "generated" && d.Project.Cover.Status == "succeeded" {
		snapshot.CoverKey = d.Keys[d.Project.Cover.ID]
	}
	d.Generations[target] = snapshot.Generation
	b, e := json.Marshal(snapshot)
	return models.DirectorJob{Base: models.Base{ID: uuid.New()}, Kind: kind, TargetID: target, Status: "queued", Snapshot: b, Cost: cost}, e
}
func (f *DirectorFacade) Mutate(c context.Context, id, user, action, target string, in req.DirectorRequest) (dto.DirectorProject, error) {
	if _, e := uuid.Parse(id); e != nil {
		return dto.DirectorProject{}, repo.ErrNotFound
	}
	ai := directorContains([]string{"cover-generate", "story", "rewrite", "storyboard", "regenerate", "retry", "add", "video", "export"}, action)
	if ai {
		if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
			return dto.DirectorProject{}, directorInvalid("idempotencyKey")
		}
	}
	if in.Revision == nil {
		return dto.DirectorProject{}, directorInvalid("revision")
	}
	key := ""
	if ai {
		key = in.IdempotencyKey
	}
	hash := storyHash(struct {
		Action, Project, Target string
		Input                   req.DirectorRequest
	}{action, id, target, in})
	row, e := f.store.Change(c, id, user, key, hash, func(m *models.DirectorProject, loadAsset func(string) (*models.LibraryAsset, error)) ([]models.DirectorJob, error) {
		d, e := directorDecode(m)
		if e != nil {
			return nil, e
		}
		p := &d.Project
		if *in.Revision != p.Revision {
			return nil, &DirectorError{Status: 409, Code: "REVISION_CONFLICT", Message: "มีการแก้ไขจากอีกแท็บ กรุณาโหลดข้อมูลล่าสุด", CurrentRevision: directorPointer(p.Revision)}
		}
		if ai && !f.Capabilities().Enabled {
			return nil, directorError(503, "DIRECTOR_UNAVAILABLE")
		}
		jobs := []models.DirectorJob{}
		enqueue := func(kind, t string, cost int) error {
			j, e := f.job(&d, kind, t, cost, in, loadAsset)
			if e == nil {
				jobs = append(jobs, j)
			}
			return e
		}
		changed := true
		switch action {
		case "project":
			changed = false
			if in.Name != nil {
				p.Name = *in.Name
				changed = true
			}
			if in.AspectRatio != nil || in.Style != nil || in.Resolution != nil {
				if p.Storyboard.Status != "idle" {
					return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
				}
				changed = true
			}
			if in.AspectRatio != nil {
				p.AspectRatio = *in.AspectRatio
			}
			if in.Resolution != nil {
				p.Resolution = *in.Resolution
			}
			if in.Style != nil {
				if p.Style != *in.Style && p.Cover != nil && p.Cover.Source == "generated" {
					delete(d.Generations, p.Cover.ID)
					p.Cover = nil
				}
				p.Style = *in.Style
			}
			if e = f.settings(p); e != nil {
				return nil, e
			}
			if in.Step != nil {
				if *in.Step != "idea" && (*in.Step != "plot" || p.Story.Status == "idle") && (*in.Step != "storyboard" || p.Storyboard.Status == "idle") {
					return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
				}
				p.Step = *in.Step
			}
		case "delete":
			m.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
			changed = false
		case "idea":
			if directorBusy(p.Story.Status) {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			if in.Prompt == nil || in.Genre == nil || in.Moods == nil || in.SceneCount == nil || in.CharacterAssetIDs == nil || in.StoryModelID == nil {
				return nil, directorInvalid("idea")
			}
			idea := dto.DirectorIdea{Prompt: *in.Prompt, Genre: *in.Genre, Moods: *in.Moods, SceneCount: *in.SceneCount, CharacterAssetIDs: *in.CharacterAssetIDs, StoryModelID: *in.StoryModelID}
			if e = f.validateIdea(idea, false, loadAsset); e != nil {
				return nil, e
			}
			p.Idea = idea
		case "cover-generate":
			changed = false
			if !directorBound(p.Idea.Prompt, 10, 1000) {
				return nil, directorInvalid("prompt")
			}
			if p.Cover != nil && directorBusy(p.Cover.Status) {
				return nil, directorError(409, "COVER_IN_PROGRESS")
			}
			model := p.Storyboard.ImageModelID
			if in.ImageModelID != nil {
				model = *in.ImageModelID
			}
			if !directorModel(f.Capabilities().Models.Image, model) {
				return nil, directorInvalid("imageModelId")
			}
			p.Cover = &dto.DirectorCover{ID: uuid.NewString(), Status: "queued", Source: "generated", ImageModelID: &model, PointsCharged: directorPointer(f.options.ImagePoints)}
			if e = enqueue("cover", p.Cover.ID, f.options.ImagePoints); e != nil {
				return nil, e
			}
		case "cover-shot":
			if p.Cover != nil && directorBusy(p.Cover.Status) {
				return nil, directorError(409, "COVER_IN_PROGRESS")
			}
			if in.ShotID == nil {
				return nil, directorInvalid("shotId")
			}
			sh := directorFindShot(p, *in.ShotID)
			if sh == nil {
				return nil, directorError(404, "SHOT_NOT_FOUND")
			}
			if sh.Image.Status != "succeeded" {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			cover := &dto.DirectorCover{ID: uuid.NewString(), Source: "shot", ShotID: directorPointer(sh.ID), Status: "succeeded", Progress: 100}
			d.Keys[cover.ID] = d.Keys[sh.ID]
			p.Cover = cover
		case "cover-delete":
			if p.Cover != nil && directorBusy(p.Cover.Status) {
				return nil, directorError(409, "COVER_IN_PROGRESS")
			}
			if p.Cover != nil {
				delete(d.Generations, p.Cover.ID)
			}
			p.Cover = nil
		case "story":
			if directorBusy(p.Story.Status) || directorBusy(p.Storyboard.Status) || directorAnyWriting(p) || directorAnyVideoBusy(p) {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			if p.Story.Status != "idle" && !in.Replace {
				return nil, directorError(409, "STORY_EXISTS")
			}
			if e = f.validateIdea(p.Idea, true, loadAsset); e != nil {
				return nil, e
			}
			d.Bible, d.Settings, d.Prompts = "", map[string]string{}, map[string]directorPlannedPrompt{}
			// Replacing the story invalidates all old scene/shot generation tokens.
			for _, sc := range p.Scenes {
				delete(d.Generations, sc.ID)
				for _, sh := range sc.Shots {
					delete(d.Generations, sh.ID)
				}
			}
			p.Scenes = []dto.DirectorScene{}
			for i := 0; i < p.Idea.SceneCount; i++ {
				p.Scenes = append(p.Scenes, dto.DirectorScene{ID: uuid.NewString(), Index: i + 1, Status: "pending", ShotCount: 3, CharacterAssetIDs: []string{}, Shots: []dto.DirectorShot{}})
			}
			p.Story = dto.DirectorStory{Status: "queued", Phase: directorPointer("analyzing"), StoryModelID: directorPointer(p.Idea.StoryModelID), TotalScenes: p.Idea.SceneCount, PointsCharged: f.options.StoryPoints}
			p.Storyboard.Status = "idle"
			p.Storyboard.TotalShots = 0
			p.Storyboard.CompletedImages = 0
			p.Step = "plot"
			if e = enqueue("story", "story", f.options.StoryPoints); e != nil {
				return nil, e
			}
		case "scene", "rewrite":
			sc := directorFindScene(p, target)
			if sc == nil {
				return nil, directorError(404, "SCENE_NOT_FOUND")
			}
			if action == "rewrite" && directorAnyWriting(p) {
				return nil, directorError(409, "SCENE_BUSY")
			}
			retryableScene := action == "rewrite" && sc.Status == "failed" && sc.CanRetry
			if (sc.Status != "ready" && !retryableScene) || directorBusy(p.Storyboard.Status) || (action == "rewrite" && directorAnyVideoBusy(p)) {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			if action == "rewrite" {
				if directorAnyWriting(p) {
					return nil, directorError(409, "SCENE_BUSY")
				}
				if in.Instruction != nil && !directorBound(*in.Instruction, 0, 300) {
					return nil, directorInvalid("instruction")
				}
				sc.Status = "writing"
				sc.DirectorFailure = dto.DirectorFailure{}
				if e = enqueue("rewrite", sc.ID, (f.options.StoryPoints+len(p.Scenes)-1)/len(p.Scenes)); e != nil {
					return nil, e
				}
			} else {
				if in.Title != nil {
					if !directorBound(*in.Title, 1, 80) {
						return nil, directorInvalid("title")
					}
					sc.Title = *in.Title
				}
				if in.Summary != nil {
					if !directorBound(*in.Summary, 1, 1200) {
						return nil, directorInvalid("summary")
					}
					sc.Summary = *in.Summary
				}
				if in.Setting != nil {
					setting := strings.TrimSpace(*in.Setting)
					if !directorBound(setting, 0, 400) {
						return nil, directorInvalid("setting")
					}
					if setting != sc.Setting {
						sc.Setting = setting
						for i := range sc.Shots {
							if sc.Shots[i].Image.Status == "succeeded" {
								sc.Shots[i].Image.Stale = true
							}
							directorMarkVideoStale(&sc.Shots[i])
						}
					}
				}
				if in.ShotCount != nil {
					if *in.ShotCount < 1 || *in.ShotCount > 6 {
						return nil, directorInvalid("shotCount")
					}
					sc.ShotCount = *in.ShotCount
				}
				total := 0
				for _, s := range p.Scenes {
					total += s.ShotCount
				}
				if total > 40 {
					return nil, directorError(409, "SHOT_LIMIT")
				}
			}
		case "settings":
			if directorBusy(p.Storyboard.Status) {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			cap := f.Capabilities()
			if in.ImageModelID != nil {
				if !directorModel(cap.Models.Image, *in.ImageModelID) {
					return nil, directorInvalid("imageModelId")
				}
				p.Storyboard.ImageModelID = *in.ImageModelID
			}
			if in.VideoModelID != nil {
				id := directorCanonicalVideoModel(*in.VideoModelID)
				if !directorModel(cap.Models.Video, id) {
					return nil, directorInvalid("videoModelId")
				}
				p.Storyboard.VideoModelID = id
			}
		case "storyboard":
			if p.Story.Status != "succeeded" || directorBusy(p.Storyboard.Status) || directorAnyVideoBusy(p) {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			if directorTotal(p) > 0 && !in.Replace {
				return nil, directorError(409, "STORYBOARD_EXISTS")
			}
			n := 0
			for _, sc := range p.Scenes {
				if sc.Status != "ready" {
					return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
				}
				n += sc.ShotCount
			}
			if n > 40 {
				return nil, directorError(409, "SHOT_LIMIT")
			}
			if p.Cover != nil && p.Cover.Source == "shot" {
				p.Cover = nil
			}
			for i := range p.Scenes {
				sc := &p.Scenes[i]
				for _, sh := range sc.Shots {
					delete(d.Generations, sh.ID)
					delete(d.Prompts, sh.ID)
				}
				sc.Shots = []dto.DirectorShot{}
				for j := 0; j < sc.ShotCount; j++ {
					sc.Shots = append(sc.Shots, dto.DirectorShot{ID: uuid.NewString(), SceneID: sc.ID, Index: j + 1, Camera: f.Capabilities().Cameras[0], CharacterAssetIDs: append([]string{}, sc.CharacterAssetIDs...), Image: dto.DirectorImage{Status: "pending"}, PointsCharged: directorPointer(f.options.ImagePoints)})
				}
			}
			p.Storyboard.Status = "queued"
			p.Storyboard.TotalShots = n
			p.Storyboard.CompletedImages = 0
			p.Storyboard.StyleReference = "style"
			if p.Cover != nil && p.Cover.Source == "generated" && p.Cover.Status == "succeeded" && len(p.Idea.CharacterAssetIDs) == 0 {
				p.Storyboard.StyleReference = "cover"
			}
			p.Step = "storyboard"
			if e = enqueue("plan", "storyboard", 0); e != nil {
				return nil, e
			}
			for _, sc := range p.Scenes {
				for _, sh := range sc.Shots {
					if e = enqueue("image", sh.ID, f.options.ImagePoints); e != nil {
						return nil, e
					}
				}
			}
		case "shot", "regenerate", "retry", "shot-delete":
			sh := directorFindShot(p, target)
			if sh == nil {
				return nil, directorError(404, "SHOT_NOT_FOUND")
			}
			if action != "shot" && directorVideoBusy(sh) {
				return nil, directorError(409, "SHOT_BUSY")
			}
			switch action {
			case "shot":
				if in.Description != nil {
					if !directorBound(*in.Description, 1, 600) {
						return nil, directorInvalid("description")
					}
					sh.Description = *in.Description
				}
				if in.Camera != nil {
					if !directorContains(f.Capabilities().Cameras, *in.Camera) {
						return nil, directorInvalid("camera")
					}
					sh.Camera = *in.Camera
				}
				if sh.Image.Status == "succeeded" || d.Keys[sh.ID] != "" {
					sh.Image.Stale = true
				}
				directorMarkVideoStale(sh)
			case "shot-delete":
				sc := directorFindScene(p, sh.SceneID)
				if len(sc.Shots) < 2 {
					return nil, directorError(409, "LAST_SHOT")
				}
				next := []dto.DirectorShot{}
				for _, s := range sc.Shots {
					if s.ID != target {
						s.Index = len(next) + 1
						next = append(next, s)
					}
				}
				sc.Shots = next
				delete(d.Generations, target)
				delete(d.Generations, directorVideoTarget(target))
				if p.Cover != nil && p.Cover.ShotID != nil && *p.Cover.ShotID == target {
					p.Cover = nil
				}
				directorRecount(p)
			default:
				if directorBusy(sh.Image.Status) {
					return nil, directorError(409, "SHOT_BUSY")
				}
				if action == "retry" && !sh.Image.CanRetry {
					return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
				}
				sh.Image.Status = "queued"
				sh.Image.Progress = 0
				sh.Image.Stale = false
				sh.Image.DirectorFailure = dto.DirectorFailure{}
				sh.PointsCharged = directorPointer(f.options.ImagePoints)
				sh.PointsRefunded = 0
				if e = enqueue("image", sh.ID, f.options.ImagePoints); e != nil {
					return nil, e
				}
				directorRecount(p)
			}
		case "add":
			sc := directorFindScene(p, target)
			if sc == nil {
				return nil, directorError(404, "SCENE_NOT_FOUND")
			}
			if sc.Status != "ready" || directorBusy(p.Storyboard.Status) || p.Storyboard.Status == "idle" {
				return nil, directorError(409, "DIRECTOR_STATE_CONFLICT")
			}
			if len(sc.Shots) >= 8 || directorTotal(p) >= 40 {
				return nil, directorError(409, "SHOT_LIMIT")
			}
			pos := len(sc.Shots)
			if in.AfterShotID != nil {
				pos = -1
				for i, s := range sc.Shots {
					if s.ID == *in.AfterShotID {
						pos = i + 1
					}
				}
				if pos < 0 {
					return nil, directorInvalid("afterShotId")
				}
			}
			sh := dto.DirectorShot{ID: uuid.NewString(), SceneID: sc.ID, Camera: f.Capabilities().Cameras[0], CharacterAssetIDs: append([]string{}, sc.CharacterAssetIDs...), Image: dto.DirectorImage{Status: "pending"}, PointsCharged: directorPointer(f.options.ImagePoints)}
			sc.Shots = append(sc.Shots, dto.DirectorShot{})
			copy(sc.Shots[pos+1:], sc.Shots[pos:])
			sc.Shots[pos] = sh
			for i := range sc.Shots {
				sc.Shots[i].Index = i + 1
			}
			if e = enqueue("add", sh.ID, f.options.ImagePoints); e != nil {
				return nil, e
			}
			directorRecount(p)
		case "video":
			changed = false
			if e = f.enqueueVideos(&d, in, enqueue); e != nil {
				return nil, e
			}
		case "export":
			changed = false
			if f.options.Processor == nil || f.options.VideoFetch == nil {
				return nil, directorError(503, "DIRECTOR_EXPORT_UNAVAILABLE")
			}
			if p.Export != nil && directorBusy(p.Export.Status) {
				return nil, directorError(409, "EXPORT_IN_PROGRESS")
			}
			keys := directorClipKeys(&d)
			if keys == nil {
				return nil, directorError(409, "VIDEO_INCOMPLETE")
			}
			p.Export = &dto.DirectorExport{Status: "queued", ClipCount: len(keys)}
			if e = enqueue("export", "export", 0); e != nil {
				return nil, e
			}
		default:
			return nil, directorInvalid("action")
		}
		if changed {
			p.Revision++
		}
		f.estimates(p)
		if e = directorEncode(m, &d); e != nil {
			return nil, e
		}
		return jobs, nil
	})
	if e != nil {
		return dto.DirectorProject{}, e
	}
	return f.present(c, row)
}

func (f *DirectorFacade) enqueueVideos(d *directorDocument, in req.DirectorRequest, enqueue func(string, string, int) error) error {
	p := &d.Project
	if !f.Capabilities().VideoEnabled {
		return directorError(503, "DIRECTOR_VIDEO_UNAVAILABLE")
	}
	if directorBusy(p.Storyboard.Status) || p.Storyboard.Status == "idle" {
		return directorError(409, "DIRECTOR_STATE_CONFLICT")
	}
	p.Storyboard.VideoModelID = directorCanonicalVideoModel(p.Storyboard.VideoModelID)
	m, ok := directorVideoModel(p.Storyboard.VideoModelID)
	if !ok || !m.SupportsImgVideo {
		return directorInvalid("videoModelId")
	}
	seconds := directorVideoSeconds(m)
	if !m.SupportsOutput(p.AspectRatio, seconds) {
		return directorError(422, "MODEL_NOT_SUPPORTED")
	}
	if in.Draft && in.Finalize {
		return directorInvalid("draft")
	}
	if (in.Draft || in.Finalize) && (!m.SupportsDraft() || f.options.DraftPoints <= 0) {
		return directorError(422, "DRAFT_NOT_SUPPORTED")
	}
	cost := f.options.VideoPoints
	if in.Draft {
		cost = f.options.DraftPoints
	}
	finalizable := func(sh *dto.DirectorShot) bool {
		draft, ok := d.VideoDrafts[sh.ID]
		return sh.Video != nil && sh.Video.Draft && sh.Video.Status == "succeeded" && !sh.Video.Stale && ok && time.Now().Before(modelark.DraftExpiresAt(draft.CreatedAt))
	}
	targets := []*dto.DirectorShot{}
	if in.ShotIDs != nil {
		if len(*in.ShotIDs) == 0 || len(*in.ShotIDs) > 40 {
			return directorInvalid("shotIds")
		}
		seen := map[string]bool{}
		for _, id := range *in.ShotIDs {
			sh := directorFindShot(p, id)
			if sh == nil {
				return directorError(404, "SHOT_NOT_FOUND")
			}
			if seen[id] {
				return directorInvalid("shotIds")
			}
			seen[id] = true
			targets = append(targets, sh)
		}
	} else {
		// Whole-story requests only fill shots without a current video, so a retry never pays twice.
		for i := range p.Scenes {
			for j := range p.Scenes[i].Shots {
				sh := &p.Scenes[i].Shots[j]
				if in.Finalize && finalizable(sh) || !in.Finalize && (sh.Video == nil || sh.Video.Status != "succeeded" || sh.Video.Stale) {
					targets = append(targets, sh)
				}
			}
		}
		if len(targets) == 0 {
			return directorError(409, "VIDEO_UP_TO_DATE")
		}
	}
	for _, sh := range targets {
		if sh.Image.Status != "succeeded" || d.Keys[sh.ID] == "" {
			return directorError(409, "STORYBOARD_INCOMPLETE")
		}
		if directorVideoBusy(sh) || directorBusy(sh.Image.Status) {
			return directorError(409, "SHOT_BUSY")
		}
		if in.Finalize && !finalizable(sh) {
			return directorError(409, "DRAFT_NOT_AVAILABLE")
		}
	}
	for _, sh := range targets {
		next := directorNextShot(p, sh)
		usesLast := !in.Finalize && next != nil && next.Image.Status == "succeeded" && d.Keys[next.ID] != ""
		sh.Video = &dto.DirectorVideo{Status: "queued", ModelID: p.Storyboard.VideoModelID, DurationSeconds: seconds, PointsCharged: cost, Draft: in.Draft, UsesLastFrame: usesLast || in.Finalize && sh.Video.UsesLastFrame}
		if e := enqueue("video", directorVideoTarget(sh.ID), cost); e != nil {
			return e
		}
	}
	return nil
}

func (f *DirectorFacade) Points(c context.Context, user string) (dto.DirectorPoints, error) {
	if _, e := uuid.Parse(user); e != nil {
		return dto.DirectorPoints{}, repo.ErrNotFound
	}
	b, e := f.store.Balance(c, user)
	if e != nil {
		return dto.DirectorPoints{}, fmt.Errorf("director points: %w", e)
	}
	return dto.DirectorPoints{Balance: b}, nil
}

// HTTP consumers classify domain failures without importing persistence packages.
type DirectorInsufficientPoints = repo.DirectorInsufficient

var (
	ErrDirectorProjectNotFound     = repo.ErrNotFound
	ErrDirectorAdmissionLimit      = repo.ErrConflict
	ErrDirectorIdempotencyConflict = repo.ErrDirectorIdempotency
)
