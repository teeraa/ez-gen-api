package facade_open_api

import (
	"context"
	"encoding/json"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type ProductionFacade struct {
	repo         *repo.ProductionRepository
	scenes       repo.SceneRepository
	jobs         repo.JobRepository
	uploads      repo.UploadRepository
	dna          DNAFacade
	jobFacade    JobFacade
	registry     *modelark.Registry
	available    bool
	supportsText bool
}

func NewProductionFacade(r *repo.ProductionRepository, s repo.SceneRepository, j repo.JobRepository, u repo.UploadRepository, d DNAFacade, jf JobFacade, registry *modelark.Registry, available bool) *ProductionFacade {
	return &ProductionFacade{repo: r, scenes: s, jobs: j, uploads: u, dna: d, jobFacade: jf, registry: registry, available: available}
}
func productionDraft() models.EditDraft {
	return models.EditDraft{Clips: []models.EditClip{}, AudioVolume: 1, TextLayers: []models.EditTextLayer{}}
}
func projectView(p *models.ProductionProject) dto.ProjectDTO {
	d := productionDraft()
	_ = json.Unmarshal(p.EditDraft, &d)
	if d.Clips == nil {
		d.Clips = []models.EditClip{}
	}
	if d.TextLayers == nil {
		d.TextLayers = []models.EditTextLayer{}
	}
	return dto.ProjectDTO{ID: p.ID.String(), Title: p.Title, Brief: p.Brief, AspectRatio: p.AspectRatio, Revision: p.Revision, EditDraft: d, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339)}
}
func (f *ProductionFacade) Create(c context.Context, user string, in req.CreateProjectRequest) (*dto.ProjectDTO, error) {
	if strings.TrimSpace(in.Brief) == "" {
		return nil, ErrValidation
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		r := []rune(strings.TrimSpace(in.Brief))
		if len(r) > 60 {
			r = r[:60]
		}
		title = string(r)
	}
	ratio := in.AspectRatio
	if ratio == "" {
		ratio = "16:9"
	}
	draft, _ := json.Marshal(productionDraft())
	uid, _ := uuid.Parse(user)
	p := &models.ProductionProject{UserID: uid, Title: title, Brief: in.Brief, AspectRatio: ratio, Revision: 1, EditDraft: draft}
	description := in.Brief
	if utf8.RuneCountInString(description) > 2000 {
		description = string([]rune(description)[:2000])
	}
	v := f.defaultSettings(ratio)
	vs, _ := json.Marshal(v)
	sceneTitle := "ซีนที่ 1"
	scene := &models.Scene{UserID: uid, Title: &sceneTitle, Description: &description, VideoSettings: vs}
	if e := f.repo.CreateWithScene(c, p, scene); e != nil {
		return nil, e
	}
	out := projectView(p)
	return &out, nil
}
func (f *ProductionFacade) List(c context.Context, user string, page, limit int) (*dto.ProjectListDTO, error) {
	ps, n, e := f.repo.List(c, user, page, limit)
	if e != nil {
		return nil, e
	}
	out := []dto.ProjectDTO{}
	for i := range ps {
		out = append(out, projectView(&ps[i]))
	}
	return &dto.ProjectListDTO{Items: out, Total: n, Page: page, Limit: limit}, nil
}
func (f *ProductionFacade) Detail(c context.Context, id, user string) (*dto.ProjectDetailDTO, error) {
	p, e := f.repo.Find(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	sc, e := f.ListScenes(c, id, user)
	if e != nil {
		return nil, e
	}
	exports, e := f.repo.Exports(c, id, user)
	if e != nil {
		return nil, e
	}
	ed := []dto.ExportDTO{}
	for i := range exports {
		ed = append(ed, *mapExportDTO(&exports[i]))
	}
	return &dto.ProjectDetailDTO{ProjectDTO: projectView(p), Scenes: sc.Items, Exports: ed}, nil
}
func (f *ProductionFacade) Update(c context.Context, id, user string, in req.UpdateProjectRequest) (*dto.ProjectDTO, error) {
	p, e := f.repo.Find(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	if in.Title != nil {
		p.Title = strings.TrimSpace(*in.Title)
		if p.Title == "" {
			return nil, ErrValidation
		}
	}
	if in.Brief != nil {
		p.Brief = *in.Brief
	}
	if in.AspectRatio != nil {
		p.AspectRatio = *in.AspectRatio
	}
	if e = f.repo.Save(c, p, in.Revision); e != nil {
		return nil, mapRepoErr(e)
	}
	p.UpdatedAt = time.Now()
	out := projectView(p)
	return &out, nil
}
func (f *ProductionFacade) Delete(c context.Context, id, user string) error {
	return mapRepoErr(f.repo.Delete(c, id, user))
}
func (f *ProductionFacade) ListScenes(c context.Context, id, user string) (*dto.ProductionSceneListDTO, error) {
	if _, e := f.repo.Find(c, id, user); e != nil {
		return nil, mapRepoErr(e)
	}
	ss, e := f.repo.Scenes(c, id, user)
	if e != nil {
		return nil, e
	}
	out := []dto.SceneDTO{}
	for _, s := range ss {
		d, e := f.dna.GetScene(c, s.ID.String(), user)
		if e != nil {
			return nil, e
		}
		out = append(out, *d)
	}
	return &dto.ProductionSceneListDTO{Items: out, Total: len(out)}, nil
}
func (f *ProductionFacade) defaultSettings(ratio string) models.VideoSettings {
	m, _ := f.registry.Lookup("violet-motion-2")
	d := 5
	if d < m.MinDuration {
		d = m.MinDuration
	}
	if d > m.MaxDuration {
		d = m.MaxDuration
	}
	if !m.SupportsOutput(ratio, d) {
		ratio = m.SupportedRatios[0]
	}
	return models.VideoSettings{Model: "violet-motion-2", Mode: "text", AspectRatio: ratio, Duration: d, Quality: "standard"}
}
func ValidateVideoSettings(v models.VideoSettings, m modelark.ModelConfig) error {
	if !m.SupportsOutput(v.AspectRatio, v.Duration) {
		return ErrModelInvalid
	}
	if v.Quality != "standard" && v.Quality != "high" {
		return ErrValidation
	}
	if v.Quality == "high" && m.MaxResolution == "720p" {
		return ErrModelInvalid
	}
	switch v.Mode {
	case "text":
	case "image_video":
		if !m.SupportsImgVideo {
			return ErrModelInvalid
		}
		if m.ProviderModelID == "dreamina-seedance-2-5-260628" {
			return ErrModelInvalid
		}
	case "image_image":
		if !m.SupportsRefImage {
			return ErrModelInvalid
		}
	default:
		return ErrValidation
	}
	return nil
}
func (f *ProductionFacade) ValidateSettings(v models.VideoSettings) error {
	m, ok := f.registry.Lookup(v.Model)
	if !ok {
		return ErrModelInvalid
	}
	return ValidateVideoSettings(v, m)
}
func (f *ProductionFacade) validateReference(c context.Context, id *string, user string) error {
	if id == nil || *id == "" {
		return nil
	}
	if _, e := uuid.Parse(*id); e != nil {
		return ErrValidation
	}
	a, e := f.repo.Asset(c, *id, user)
	if e != nil {
		return mapRepoErr(e)
	}
	if a.StorageKey == nil || a.MediaType == nil || (*a.MediaType != "image" && !strings.HasPrefix(*a.MediaType, "image/")) {
		return ErrInvalidFileType
	}
	return nil
}
func (f *ProductionFacade) CreateScene(c context.Context, pid, user string, in req.CreateProductionSceneRequest) (*dto.SceneDTO, error) {
	p, e := f.repo.Find(c, pid, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	v := f.defaultSettings(p.AspectRatio)
	if in.VideoSettings != nil {
		v = *in.VideoSettings
	}
	if e = f.ValidateSettings(v); e != nil {
		return nil, e
	}
	if e = f.validateReference(c, in.ReferenceAssetID, user); e != nil {
		return nil, e
	}
	if in.ReferenceAssetID != nil && v.Mode == "text" {
		return nil, ErrImageRequired
	}
	vs, _ := json.Marshal(v)
	uid, _ := uuid.Parse(user)
	s := &models.Scene{UserID: uid, ProjectID: &p.ID, Title: &in.Title, Description: &in.Description, VideoSettings: vs}
	if in.ReferenceAssetID != nil {
		a, _ := uuid.Parse(*in.ReferenceAssetID)
		s.ReferenceAssetID = &a
	}
	if e = f.repo.AddScene(c, s); e != nil {
		return nil, mapRepoErr(e)
	}
	return f.dna.GetScene(c, s.ID.String(), user)
}
func (f *ProductionFacade) Duplicate(c context.Context, id, user string) (*dto.SceneDTO, error) {
	s, e := f.scenes.FindByID(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	if s.ProjectID == nil {
		return nil, ErrValidation
	}
	if e := f.repo.ProjectForScene(c, s, user); e != nil {
		return nil, mapRepoErr(e)
	}
	copyScene := *s
	copyScene.Base = models.Base{}
	copyScene.SelectedJobID = nil
	title := strVal(s.Title) + " (สำเนา)"
	copyScene.Title = &title
	if e := f.repo.AddScene(c, &copyScene); e != nil {
		return nil, mapRepoErr(e)
	}
	return f.dna.GetScene(c, copyScene.ID.String(), user)
}
func (f *ProductionFacade) Reorder(c context.Context, id, user string, ids []string) (*dto.ProductionSceneListDTO, error) {
	if e := f.repo.Reorder(c, id, user, ids); e != nil {
		return nil, mapRepoErr(e)
	}
	return f.ListScenes(c, id, user)
}
func (f *ProductionFacade) SceneJobs(c context.Context, id, user string, page, limit int) (*dto.JobListDTO, error) {
	s, e := f.scenes.FindByID(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	if e = f.repo.ProjectForScene(c, s, user); e != nil {
		return nil, mapRepoErr(e)
	}
	js, n, e := f.repo.SceneJobs(c, id, user, page, limit)
	if e != nil {
		return nil, e
	}
	out := []dto.JobDTO{}
	for _, j := range js {
		d, e := f.jobFacade.GetJob(c, j.ID.String(), user)
		if e != nil {
			return nil, e
		}
		out = append(out, *d)
	}
	return &dto.JobListDTO{Items: out, Total: int(n), Page: page, Limit: limit}, nil
}
func validTrim(start, end float64, durationMs int64) bool {
	if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) || start < 0 || end < 0 || end > 0 && end <= start {
		return false
	}
	if durationMs > 0 {
		d := float64(durationMs) / 1000
		if start >= d || end > d+0.05 {
			return false
		}
	}
	return true
}
func (f *ProductionFacade) SaveEditor(c context.Context, id, user string, in req.SaveEditorRequest) (*dto.ProjectDTO, error) {
	p, e := f.repo.Find(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	if len(in.EditDraft.Clips) > 100 || len(in.EditDraft.TextLayers) > 100 || in.EditDraft.AudioVolume < 0 || in.EditDraft.AudioVolume > 2 || math.IsNaN(in.EditDraft.AudioVolume) {
		return nil, ErrValidation
	}
	seen := map[string]bool{}
	for _, clip := range in.EditDraft.Clips {
		if seen[clip.SceneID] {
			return nil, ErrValidation
		}
		seen[clip.SceneID] = true
		s, e := f.scenes.FindByID(c, clip.SceneID, user)
		if e != nil || s.ProjectID == nil || s.ProjectID.String() != id {
			return nil, ErrNotFound
		}
		var duration int64
		if clip.JobID != nil {
			j, e := f.jobs.FindByID(c, *clip.JobID, user)
			if e != nil {
				return nil, mapRepoErr(e)
			}
			if !isSceneVideo(j, s.ID) {
				return nil, ErrSceneHasNoOutput
			}
			if j.OutputDurationMs != nil {
				duration = int64(*j.OutputDurationMs)
			}
		}
		if !validTrim(clip.TrimStart, clip.TrimEnd, duration) {
			return nil, ErrValidation
		}
	}
	if in.EditDraft.AudioUploadID != nil {
		u, e := f.uploads.FindByID(c, *in.EditDraft.AudioUploadID, user)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if !strings.HasPrefix(u.MimeType, "audio/") {
			return nil, ErrInvalidFileType
		}
	}
	for _, t := range in.EditDraft.TextLayers {
		if !seen[t.SceneID] || strings.TrimSpace(t.Text) == "" || utf8.RuneCountInString(t.Text) > 200 {
			return nil, ErrValidation
		}
	}
	p.EditDraft, e = json.Marshal(in.EditDraft)
	if e != nil {
		return nil, ErrValidation
	}
	if e = f.repo.Save(c, p, in.Revision); e != nil {
		return nil, mapRepoErr(e)
	}
	p.UpdatedAt = time.Now()
	out := projectView(p)
	return &out, nil
}
func isSceneVideo(j *models.VideoGenerationJob, sid uuid.UUID) bool {
	return j.SceneID != nil && *j.SceneID == sid && j.Mode != "text_image" && j.Status == models.JobStatusSucceeded && j.OutputMediaKey != nil && (j.OutputMediaType == nil || *j.OutputMediaType == "video" || strings.HasPrefix(*j.OutputMediaType, "video/"))
}
func (f *ProductionFacade) Capabilities() dto.VideoCapabilitiesDTO {
	out := dto.VideoCapabilitiesDTO{Available: f.available, Models: []dto.VideoModelDTO{}, Export: dto.ExportCapabilitiesDTO{Formats: []string{"mp4"}, Resolutions: []string{"720p", "1080p"}, AspectRatios: []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}}}
	_, e := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	out.Export.Available = e == nil && e2 == nil
	out.Export.SupportsTextOverlays = f.supportsText
	routes := f.registry.Routes()
	ids := []string{}
	for id := range routes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m, _ := f.registry.Lookup(id)
		qs := []string{"standard"}
		if m.MaxResolution != "720p" {
			qs = append(qs, "high")
		}
		out.Models = append(out.Models, dto.VideoModelDTO{ID: id, Name: m.DisplayName, ProviderModel: m.ProviderModelID, AspectRatios: m.SupportedRatios, MinDuration: m.MinDuration, MaxDuration: m.MaxDuration, Qualities: qs, SupportsImageToVideo: m.SupportsImgVideo && m.ProviderModelID != "dreamina-seedance-2-5-260628", SupportsReferenceImage: m.SupportsRefImage, SupportsAudio: m.SupportsAudio})
	}
	return out
}
