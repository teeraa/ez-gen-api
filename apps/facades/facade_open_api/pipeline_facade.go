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
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

type pipelineStore interface {
	Create(context.Context, *models.StudioPipeline) (*models.StudioPipeline, error)
	ByKey(context.Context, string, string) (*models.StudioPipeline, error)
	Get(context.Context, string, string) (*models.StudioPipeline, error)
	List(context.Context, string, int, int) ([]models.StudioPipeline, int64, error)
	Claim(context.Context) (*models.StudioPipeline, error)
	Advance(context.Context, *models.StudioPipeline) error
	Retry(context.Context, string, string) error
	InsertAsset(context.Context, *models.LibraryAsset) error
}
type pipelineImages interface {
	PipelineImageModel() string
	CreatePipelineImage(context.Context, string, req.CreateImageRequest, string, string) (*dto.CreateJobResponseDTO, error)
	CreateImage(context.Context, string, req.CreateImageRequest) (*dto.CreateJobResponseDTO, error)
	Capabilities() dto.ImageCapabilitiesDTO
}
type pipelineJobReader interface {
	FindByID(context.Context, string, string) (*models.VideoGenerationJob, error)
	FindByIdempotencyKey(context.Context, string, string) (*models.VideoGenerationJob, error)
}

func pipelineRenderingVersion(visualStyle string) string {
	if visualStyle == "" || visualStyle == "auto" || visualStyle == "cinematic_realistic" {
		return promptcompiler.PipelineFilmedHumanVideoVersion
	}
	return characterdesign.PipelineCompilerVersion
}

type pipelineSnapshot struct {
	SourceDraftID    string                    `json:"sourceDraftId,omitempty"`
	SourceDraftJobID string                    `json:"sourceDraftJobId,omitempty"`
	SourceMode       string                    `json:"sourceMode,omitempty"`
	RenderingVersion string                    `json:"renderingVersion,omitempty"`
	Request          req.CreatePipelineRequest `json:"request"`
	ImageModel       string                    `json:"imageModel"`
	VideoModel       modelark.ModelConfig      `json:"videoModel"`
}
type PipelineFacade struct {
	store        pipelineStore
	images       pipelineImages
	jobs         JobFacade
	reader       pipelineJobReader
	assets       repo.StudioRepository
	uploads      repo.UploadRepository
	registry     modelRegistry
	storage      mediaStorage
	videoEnabled bool
}

func NewPipelineFacade(s pipelineStore, i pipelineImages, j JobFacade, r pipelineJobReader, a repo.StudioRepository, u repo.UploadRepository, m modelRegistry, storage mediaStorage, enabled bool) *PipelineFacade {
	return &PipelineFacade{store: s, images: i, jobs: j, reader: r, assets: a, uploads: u, registry: m, storage: storage, videoEnabled: enabled}
}
func pipelineOutputSupported(image dto.ImageCapabilitiesDTO, m modelark.ModelConfig, ratio string, duration int) bool {
	if duration < 4 || duration > 15 || ImageSize(ratio) == "" || !m.SupportsOutput(ratio, duration) {
		return false
	}
	for _, supported := range image.AspectRatios {
		if ratio == supported {
			return true
		}
	}
	return false
}

// pipelineCompileInputs is the prompt Create validates; Preflight measures the same one.
func pipelineCompileInputs(in req.CreatePipelineRequest) promptcompiler.Inputs {
	return promptcompiler.Inputs{PipelineRenderingVersion: pipelineRenderingVersion(in.VisualStyle), Dialogue: in.Dialogue, ModelID: in.ModelID, Quality: in.Quality, Scene: &promptcompiler.SceneInputs{Description: in.ScenePrompt}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "generated/image.png"}}}
}

// Preflight applies Create's text and settings checks without touching storage, so the studio
// can stop a request the server would reject before the user sends it.
func (f *PipelineFacade) Preflight(in req.CreatePipelineRequest) dto.PipelinePreflightDTO {
	out := dto.PipelinePreflightDTO{Issues: []dto.PipelinePreflightIssue{}}
	add := func(field, reason, message string) {
		out.Issues = append(out.Issues, dto.PipelinePreflightIssue{Field: field, Reason: reason, Message: message})
	}
	for _, field := range []struct {
		name, label, value string
		max                int
	}{{"characterPrompt", "ลักษณะตัวละคร", in.CharacterPrompt, 2000}, {"scenePrompt", "ฉาก", in.ScenePrompt, 2000}, {"dialogue", "บทพูด", in.Dialogue, 1000}} {
		if n := utf8.RuneCountInString(field.value); n > field.max {
			add(field.name, "too_long", fmt.Sprintf("%sยาวเกิน %d ตัวอักษร (เกิน %d)", field.label, field.max, n-field.max))
		}
	}
	if quoted, _ := json.Marshal(in.Dialogue); utf8.RuneCount(quoted) > 1700 {
		add("dialogue", "dialogue_encoding_budget", "บทพูดมีเครื่องหมายคำพูดหรือการขึ้นบรรทัดใหม่มากเกินไป")
	}
	m, ok := f.registry.Lookup(in.ModelID)
	if !ok {
		add("modelId", "invalid_option", "ไม่พบโมเดลที่เลือก")
		return out
	}
	if in.Dialogue != "" && !m.SupportsAudio {
		add("dialogue", "unsupported", "โมเดลนี้ไม่รองรับบทพูด เลือกโมเดลที่มีเสียงหรือเว้นบทพูดว่างไว้")
	}
	if in.Draft && !m.SupportsDraft() {
		add("draft", "unsupported", "โมเดลนี้ไม่มีโหมดแบบร่าง")
	}
	if in.AspectRatio != "" && in.DurationSeconds != 0 && !m.SupportsOutput(in.AspectRatio, in.DurationSeconds) {
		add("aspectRatio", "unsupported", "โมเดลนี้ไม่รองรับสัดส่วนหรือความยาวที่เลือก")
	}
	if utf8.RuneCountInString(in.Dialogue) <= 1000 && (in.Dialogue == "" || m.SupportsAudio) {
		used, limit := promptcompiler.PromptUsage(pipelineCompileInputs(in), m)
		out.PromptRunes, out.PromptLimit = used, limit
		if used > limit {
			if in.Dialogue != "" {
				add("scenePrompt", "prompt_budget", fmt.Sprintf("ฉากและบทพูดรวมกันยาวเกินไป %d ตัวอักษร กรุณาย่อฉากหรือบทพูด", used-limit))
			} else {
				out.SceneTruncated = true
			}
		}
	}
	out.OK = len(out.Issues) == 0
	return out
}

func (f *PipelineFacade) Capabilities() dto.PipelineCapabilitiesDTO {
	d := f.modelCapabilities("violet-motion-2")
	d.Models = []dto.PipelineModelCapabilitiesDTO{}
	for _, id := range []string{"violet-motion-2", "violet-cinema", "violet-fast", "violet-realism", "violet-classic"} {
		if model, ok := f.registry.Lookup(id); ok {
			caps := f.modelCapabilities(id)
			d.Models = append(d.Models, dto.PipelineModelCapabilitiesDTO{ID: id, Label: model.PublicLabel(), SupportsDraft: model.SupportsDraft(), PipelineCapabilitiesDTO: caps})
		}
	}
	return d
}
func (f *PipelineFacade) modelCapabilities(id string) dto.PipelineCapabilitiesDTO {
	m, ok := f.registry.Lookup(id)
	image := f.images.Capabilities()
	d := dto.PipelineCapabilitiesDTO{Enabled: f.videoEnabled && image.SupportsCharacterReferenceImage && ok && m.SupportsRefImage, SupportsDialogue: ok && m.SupportsAudio, AspectRatios: []string{}, Durations: []int{}}
	if ok {
		for _, ratio := range m.SupportedRatios {
			for duration := 4; duration <= 15; duration++ {
				if pipelineOutputSupported(image, m, ratio, duration) {
					d.AspectRatios = append(d.AspectRatios, ratio)
					break
				}
			}
		}
		for duration := 4; duration <= 15; duration++ {
			for _, ratio := range d.AspectRatios {
				if pipelineOutputSupported(image, m, ratio, duration) {
					d.Durations = append(d.Durations, duration)
					break
				}
			}
		}
	}
	d.TextEnabled = f.videoEnabled && image.Enabled && ok && m.SupportsRefImage && len(d.AspectRatios) > 0 && len(d.Durations) > 0
	d.LibraryAspectRatios = []string{}
	d.LibraryDurations = []int{}
	if ok {
		for _, ratio := range m.SupportedRatios {
			if ImageSize(ratio) != "" {
				d.LibraryAspectRatios = append(d.LibraryAspectRatios, ratio)
			}
		}
		for duration := 4; duration <= 15; duration++ {
			for _, ratio := range d.LibraryAspectRatios {
				if m.SupportsOutput(ratio, duration) {
					d.LibraryDurations = append(d.LibraryDurations, duration)
					break
				}
			}
		}
	}
	d.LibraryEnabled = f.videoEnabled && ok && m.SupportsRefImage && len(d.LibraryAspectRatios) > 0 && len(d.LibraryDurations) > 0
	d.Enabled = d.Enabled && len(d.AspectRatios) > 0 && len(d.Durations) > 0
	if !d.Enabled {
		reason := "ยังไม่พร้อมสร้างภาพและวิดีโอ กรุณาลองใหม่ภายหลัง"
		d.UnavailableReason = &reason
	}
	return d
}
func pipelineSource(in req.CreatePipelineRequest) string {
	if in.CharacterAssetID != "" {
		return "library"
	}
	if in.ReferenceImageID != "" {
		return "upload"
	}
	return "text"
}
func normalizePipeline(in req.CreatePipelineRequest) req.CreatePipelineRequest {
	in.CharacterPrompt = strings.TrimSpace(in.CharacterPrompt)
	in.ScenePrompt = strings.TrimSpace(in.ScenePrompt)
	if in.VisualStyle == "" {
		in.VisualStyle = "cinematic_realistic"
	}
	if in.AspectRatio == "" {
		in.AspectRatio = "9:16"
	}
	if in.DurationSeconds == 0 {
		in.DurationSeconds = 5
	}
	if in.ModelID == "" {
		in.ModelID = "violet-motion-2"
	}
	if in.Quality == "" {
		in.Quality = "standard"
	}
	return in
}
func (f *PipelineFacade) Create(c context.Context, user string, in req.CreatePipelineRequest) (*dto.PipelineDTO, error) {
	in = normalizePipeline(in)
	uid, e := uuid.Parse(user)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if in.ReferenceImageID != "" {
		if _, e = uuid.Parse(in.ReferenceImageID); e != nil {
			return nil, pipelineValidation("referenceImageId", "invalid_uuid", "รหัสภาพอ้างอิงไม่ถูกต้อง กรุณาแนบภาพใหม่")
		}
	}
	if in.CharacterAssetID != "" {
		if _, e = uuid.Parse(in.CharacterAssetID); e != nil {
			return nil, pipelineValidation("characterAssetId", "invalid_uuid", "รหัสตัวละครไม่ถูกต้อง กรุณาเลือกตัวละครจากคลังอีกครั้ง")
		}
	}
	if in.CharacterAssetID != "" && in.ReferenceImageID != "" {
		return nil, pipelineValidation("characterAssetId", "conflicting_sources", "กรุณาเลือกตัวละครจากคลังหรือแนบภาพใหม่เพียงอย่างเดียว")
	}
	if _, e = uuid.Parse(in.IdempotencyKey); e != nil {
		return nil, pipelineValidation("idempotencyKey", "invalid_uuid", "รหัสคำขอไม่ถูกต้อง กรุณาเริ่มคำขอใหม่")
	}
	quoted, _ := json.Marshal(in.Dialogue)
	if in.CharacterPrompt == "" && in.CharacterAssetID == "" {
		return nil, pipelineValidation("characterPrompt", "required", "กรุณาระบุรายละเอียดตัวละคร")
	}
	if in.ScenePrompt == "" {
		return nil, pipelineValidation("scenePrompt", "required", "กรุณาระบุฉากและการเคลื่อนไหว")
	}
	for _, field := range []struct {
		name, value string
		max         int
	}{{"characterPrompt", in.CharacterPrompt, 2000}, {"scenePrompt", in.ScenePrompt, 2000}, {"dialogue", in.Dialogue, 1000}} {
		if utf8.RuneCountInString(field.value) > field.max {
			return nil, pipelineValidation(field.name, "too_long", "ข้อความยาวเกินขีดจำกัด กรุณาย่อข้อความแล้วลองอีกครั้ง")
		}
	}
	if utf8.RuneCount(quoted) > 1700 {
		return nil, pipelineValidation("dialogue", "dialogue_encoding_budget", "บทพูดมีข้อความหรืออักขระพิเศษมากเกินไป กรุณาลดความยาว เครื่องหมายคำพูด หรือการขึ้นบรรทัดใหม่")
	}

	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	// Replays do not depend on expiring uploads or changed runtime capabilities.
	old, e := f.store.ByKey(c, in.IdempotencyKey, user)
	if e == nil {
		if old.RequestHash != hash {
			return nil, ErrIdempotencyMismatch
		}
		return f.view(c, old)
	}
	if !errors.Is(e, repo.ErrNotFound) {
		return nil, e
	}
	source := pipelineSource(in)
	caps := f.modelCapabilities(in.ModelID)
	if (source == "upload" && !caps.Enabled) || (source == "text" && !caps.TextEnabled) || (source == "library" && !caps.LibraryEnabled) {
		return nil, ErrImageUnavailable
	}
	m, ok := f.registry.Lookup(in.ModelID)
	if !ok || !m.SupportsRefImage || !m.SupportsOutput(in.AspectRatio, in.DurationSeconds) || (source != "library" && !pipelineOutputSupported(f.images.Capabilities(), m, in.AspectRatio, in.DurationSeconds)) || (in.Dialogue != "" && !m.SupportsAudio) {
		return nil, ErrModelInvalid
	}
	if in.Draft && !m.SupportsDraft() {
		return nil, ErrModelInvalid
	}
	if e := ValidateVideoSettings(models.VideoSettings{Model: in.ModelID, Mode: "image_image", AspectRatio: in.AspectRatio, Duration: in.DurationSeconds, Quality: in.Quality}, m); e != nil {
		return nil, e
	}
	if _, err := promptcompiler.Compile(pipelineCompileInputs(in), m); err != nil {
		if errors.Is(err, promptcompiler.ErrPromptBudget) {
			return nil, pipelineValidation("scenePrompt", "prompt_budget", "รายละเอียดฉากและบทพูดรวมกันยาวเกินขีดจำกัด กรุณาย่อฉากหรือบทพูดแล้วลองอีกครั้ง")
		}
		return nil, pipelineValidation("scenePrompt", "invalid_option", "รายละเอียดวิดีโอไม่รองรับ กรุณาตรวจสอบฉาก บทพูด และการตั้งค่า")
	}
	if _, err := characterdesign.Normalize(characterdesign.Options{VisualStyle: in.VisualStyle, DesignFormat: "full_body"}); err != nil {
		return nil, pipelineValidation("visualStyle", "invalid_option", "สไตล์ภาพไม่รองรับ กรุณาเลือกสไตล์อีกครั้ง")
	}
	stage := "image"
	imageModel := f.images.PipelineImageModel()
	var assetID *uuid.UUID
	if source == "upload" {
		u, err := f.uploads.FindByID(c, in.ReferenceImageID, user)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if u.ExpiresAt.Before(time.Now()) {
			return nil, ErrImageExpired
		}
		if !strings.HasPrefix(u.MimeType, "image/") {
			return nil, pipelineValidation("referenceImageId", "invalid_image", "ไฟล์อ้างอิงต้องเป็นภาพ กรุณาแนบภาพใหม่")
		}
	} else if source == "library" {
		a, err := f.assets.Asset(c, in.CharacterAssetID, user)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if err := validatePipelineCharacter(c, a, user, f.reader); err != nil {
			return nil, err
		}
		assetID = &a.ID
		stage = "video"
		imageModel = ""
	}
	snapshot, _ := json.Marshal(pipelineSnapshot{SourceMode: source, RenderingVersion: pipelineRenderingVersion(in.VisualStyle), Request: in, ImageModel: imageModel, VideoModel: m})
	p, e := f.store.Create(c, &models.StudioPipeline{UserID: uid, IdempotencyKey: in.IdempotencyKey, RequestHash: hash, Inputs: snapshot, Status: "queued", Stage: stage, CharacterAssetID: assetID})
	if e != nil {
		if errors.Is(e, repo.ErrConflict) {
			return nil, ErrRateLimit
		}
		return nil, e
	}
	if p.RequestHash != hash {
		return nil, ErrIdempotencyMismatch
	}
	return f.view(c, p)
}
func (f *PipelineFacade) Get(c context.Context, id, user string) (*dto.PipelineDTO, error) {
	p, e := f.store.Get(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	return f.view(c, p)
}
func (f *PipelineFacade) List(c context.Context, user string, page, limit int) (*dto.PipelineListDTO, error) {
	rows, n, e := f.store.List(c, user, page, limit)
	if e != nil {
		return nil, e
	}
	d := &dto.PipelineListDTO{Items: []dto.PipelineDTO{}, Total: int(n), Page: page, Limit: limit}
	for i := range rows {
		v, e := f.view(c, &rows[i])
		if e != nil {
			return nil, e
		}
		d.Items = append(d.Items, *v)
	}
	return d, nil
}
func (f *PipelineFacade) Retry(c context.Context, id, user string) (*dto.PipelineDTO, error) {
	p, err := f.store.Get(c, id, user)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if pipelineRequiresNewRequest(strValOrDefault(p.FailureCode, "")) {
		return nil, ErrConflict
	}
	if e := f.store.Retry(c, id, user); e != nil {
		return nil, mapRepoErr(e)
	}
	return f.Get(c, id, user)
}

func (f *PipelineFacade) draftForFinal(c context.Context, p *models.StudioPipeline, frozen pipelineSnapshot) (*models.VideoGenerationJob, error) {
	if !f.videoEnabled {
		return nil, ErrImageUnavailable
	}
	if p.Status != "succeeded" || !frozen.Request.Draft || !frozen.VideoModel.SupportsDraft() || p.VideoJobID == nil || p.CharacterAssetID == nil {
		return nil, ErrConflict
	}
	job, err := f.reader.FindByID(c, p.VideoJobID.String(), p.UserID.String())
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if job.Status != models.JobStatusSucceeded || !job.IsDraft || job.ProviderTaskID == nil || *job.ProviderTaskID == "" {
		return nil, ErrConflict
	}
	if !modelark.DraftExpiresAt(job.CreatedAt).After(time.Now()) {
		return nil, ErrDraftExpired
	}
	var provenance struct {
		ProviderModelID string `json:"providerModelId"`
	}
	if json.Unmarshal(job.EffectiveInputs, &provenance) != nil || provenance.ProviderModelID != frozen.VideoModel.ProviderModelID {
		return nil, ErrConflict
	}
	return job, nil
}

func (f *PipelineFacade) Finalize(c context.Context, id, user string, in req.FinalizePipelineRequest) (*dto.PipelineDTO, error) {
	if _, err := uuid.Parse(in.IdempotencyKey); err != nil {
		return nil, pipelineValidation("idempotencyKey", "invalid_uuid", "รหัสคำขอไม่ถูกต้อง กรุณาเริ่มคำขอใหม่")
	}
	source, err := f.store.Get(c, id, user)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	var frozen pipelineSnapshot
	if json.Unmarshal(source.Inputs, &frozen) != nil {
		return nil, ErrValidation
	}
	// Replay remains available after the provider's draft retention window ends.
	old, err := f.store.ByKey(c, in.IdempotencyKey, user)
	if err == nil {
		var previous pipelineSnapshot
		if json.Unmarshal(old.Inputs, &previous) != nil || previous.SourceDraftID != id {
			return nil, ErrIdempotencyMismatch
		}
		return f.view(c, old)
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	draft, err := f.draftForFinal(c, source, frozen)
	if err != nil {
		return nil, err
	}
	frozen.SourceDraftID = id
	frozen.SourceDraftJobID = draft.ID.String()
	frozen.SourceMode = "library"
	frozen.Request.Draft = false
	frozen.Request.Quality = "high"
	frozen.Request.CharacterAssetID = source.CharacterAssetID.String()
	frozen.Request.ReferenceImageID = ""
	frozen.Request.IdempotencyKey = in.IdempotencyKey
	frozen.ImageModel = ""
	snapshot, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(snapshot)
	hash := hex.EncodeToString(sum[:])
	p, err := f.store.Create(c, &models.StudioPipeline{UserID: source.UserID, IdempotencyKey: in.IdempotencyKey, RequestHash: hash, Inputs: snapshot, Status: "queued", Stage: "video", CharacterAssetID: source.CharacterAssetID})
	if err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, ErrRateLimit
		}
		return nil, err
	}
	if p.RequestHash != hash {
		return nil, ErrIdempotencyMismatch
	}
	return f.view(c, p)
}

func pipelineID(p *models.StudioPipeline, label string) uuid.UUID {
	return uuid.NewSHA1(p.ID, []byte(label))
}
func pipelineKey(p *models.StudioPipeline, kind string) string { return pipelineID(p, kind).String() }
func (f *PipelineFacade) view(c context.Context, p *models.StudioPipeline) (*dto.PipelineDTO, error) {
	// Projection must never mutate a repository-owned aggregate or reconcile stored state.
	projected := *p
	p = &projected
	historicalTerminal := false
	if p.Status == "needs_attention" {
		childID := p.VideoJobID
		if p.Stage == "image" {
			childID = p.CharacterJobID
		}
		if childID != nil {
			if child, err := f.reader.FindByID(c, childID.String(), p.UserID.String()); err == nil && pipelineConfirmedTerminal(child) {
				_ = pipelineChild(p, child, p.Stage)
				historicalTerminal = p.Status == "failed"
			}
		}
	}
	var frozen pipelineSnapshot
	_ = json.Unmarshal(p.Inputs, &frozen)
	d := &dto.PipelineDTO{IsDraft: frozen.Request.Draft, ModelID: frozen.Request.ModelID, ModelLabel: frozen.VideoModel.PublicLabel(), AspectRatio: frozen.Request.AspectRatio, DurationSeconds: frozen.Request.DurationSeconds, Quality: frozen.Request.Quality, SourceMode: pipelineSource(frozen.Request), CharacterPrompt: frozen.Request.CharacterPrompt, ScenePrompt: frozen.Request.ScenePrompt, Dialogue: frozen.Request.Dialogue, ID: p.ID.String(), Status: p.Status, Stage: p.Stage, CanRetry: p.Status == "failed" && (p.Stage == "save_character" || p.Stage == "save_video" || (p.Stage == "video" && p.VideoJobID != nil)), FailureCode: p.FailureCode, FailureMessage: p.FailureMessage, CharacterJobID: pipelineStringID(p.CharacterJobID), CharacterAssetID: pipelineStringID(p.CharacterAssetID), VideoJobID: pipelineStringID(p.VideoJobID), VideoAssetID: pipelineStringID(p.VideoAssetID), CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339)}
	if historicalTerminal || pipelineRequiresNewRequest(strValOrDefault(p.FailureCode, "")) {
		d.CanRetry = false
	}
	if frozen.SourceDraftID != "" {
		d.SourceDraftID = &frozen.SourceDraftID
	}
	if frozen.Request.Draft {
		if p.VideoJobID != nil {
			if job, err := f.reader.FindByID(c, p.VideoJobID.String(), p.UserID.String()); err == nil {
				expiry := modelark.DraftExpiresAt(job.CreatedAt).UTC().Format(time.RFC3339)
				d.DraftExpiresAt = &expiry
			}
		}
		if _, err := f.draftForFinal(c, p, frozen); err == nil {
			d.CanCreateFinal = true
		} else if errors.Is(err, ErrDraftExpired) {
			reason := "ฉบับร่างหมดอายุสำหรับสร้างฉบับสมบูรณ์ กรุณาสร้างวิดีโอใหม่"
			d.FinalUnavailableReason = &reason
		} else if p.Status == "succeeded" {
			reason := "ฉบับร่างนี้ยังใช้สร้างฉบับสมบูรณ์ไม่ได้ กรุณาสร้างวิดีโอใหม่"
			d.FinalUnavailableReason = &reason
		}
	}
	for _, entry := range []struct {
		id          *uuid.UUID
		url, expiry **string
	}{{p.CharacterAssetID, &d.CharacterMediaURL, &d.CharacterMediaURLExpiresAt}, {p.VideoAssetID, &d.VideoMediaURL, &d.VideoMediaURLExpiresAt}} {
		if entry.id == nil {
			continue
		}
		a, e := f.assets.Asset(c, entry.id.String(), p.UserID.String())
		if errors.Is(e, repo.ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if a.StorageKey == nil {
			continue
		}
		u, exp, e := f.storage.SignedURL(c, *a.StorageKey, time.Hour)
		if e != nil {
			return nil, e
		}
		t := exp.UTC().Format(time.RFC3339)
		*entry.url = &u
		*entry.expiry = &t
	}
	return d, nil
}
func pipelineStringID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
func (f *PipelineFacade) RunOnce(c context.Context) error {
	p, e := f.store.Claim(c)
	if errors.Is(e, repo.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	work, cancel := context.WithTimeout(c, 2*time.Minute)
	defer cancel()
	var frozen pipelineSnapshot
	if json.Unmarshal(p.Inputs, &frozen) != nil || (pipelineSource(frozen.Request) != "library" && frozen.ImageModel == "") {
		return f.failPipeline(c, p, "failed", "INVALID_INPUT", "ไม่พบข้อมูลคำขอ กรุณาเริ่มใหม่")
	}
	e = f.advance(work, p, frozen)
	if e != nil {
		if errors.Is(e, ErrRateLimit) {
			p.Status = "queued"
			return f.store.Advance(c, p)
		}
		if errors.Is(e, ErrDraftExpired) {
			return f.failPipeline(c, p, "failed", "DRAFT_EXPIRED", "ฉบับร่างหมดอายุ กรุณาสร้างวิดีโอใหม่")
		}
		if errors.Is(e, ErrImageExpired) || errors.Is(e, ErrValidation) || errors.Is(e, ErrModelInvalid) || errors.Is(e, ErrNotFound) || errors.Is(e, ErrAssetSource) || errors.Is(e, ErrImageUnavailable) {
			return f.failPipeline(c, p, "failed", "INPUT_UNAVAILABLE", "ข้อมูลหรือภาพอ้างอิงนี้ใช้สร้างงานไม่ได้ กรุณาตรวจสอบแล้วเริ่มคำขอใหม่")
		}
		return e
	}
	return f.store.Advance(c, p)
}
func (f *PipelineFacade) failPipeline(c context.Context, p *models.StudioPipeline, status, code, message string) error {
	p.Status = status
	p.FailureCode = &code
	p.FailureMessage = &message
	return f.store.Advance(c, p)
}
func (f *PipelineFacade) advance(c context.Context, p *models.StudioPipeline, frozen pipelineSnapshot) error {
	in := frozen.Request
	user := p.UserID.String()
	switch p.Stage {
	case "image":
		if p.CharacterJobID == nil {
			key := pipelineKey(p, "image")
			var reference *string
			if in.ReferenceImageID != "" {
				reference = &in.ReferenceImageID
			}
			j, e := f.images.CreatePipelineImage(c, user, req.CreateImageRequest{Category: "character", DesignFormat: "full_body", VisualStyle: in.VisualStyle, Prompt: in.CharacterPrompt, AspectRatio: in.AspectRatio, ReferenceImageID: reference, IdempotencyKey: key}, frozen.ImageModel, frozen.RenderingVersion)
			if e != nil {
				return e
			}
			id, e := uuid.Parse(j.JobID)
			if e != nil {
				return e
			}
			p.CharacterJobID = &id
			return nil
		}
		j, e := f.reader.FindByID(c, p.CharacterJobID.String(), user)
		if e != nil {
			return e
		}
		return pipelineChild(p, j, "save_character")
	case "save_character":
		id, e := f.saveAsset(c, p, p.CharacterJobID, "character", "ตัวละครสังเคราะห์")
		if e != nil {
			return f.markSaveFailure(p)
		}
		p.CharacterAssetID = &id
		p.Stage = "video"
	case "video":
		if p.VideoJobID == nil {
			key := pipelineKey(p, fmt.Sprintf("video:%d", p.VideoAttempt))
			asset := pipelineStringID(p.CharacterAssetID)
			if asset == nil {
				return ErrAssetSource
			}
			input := CreateJobInput{pipelineRenderingVersion: frozen.RenderingVersion, PinnedModel: &frozen.VideoModel, Draft: in.Draft, Prompt: in.ScenePrompt, Dialogue: in.Dialogue, ReferenceAssetID: asset, Mode: "image_image", ModelID: in.ModelID, AspectRatio: in.AspectRatio, DurationSeconds: in.DurationSeconds, Quality: in.Quality, IdempotencyKey: &key}
			if frozen.SourceDraftJobID != "" {
				input.draftSourceJobID = frozen.SourceDraftJobID
				input.ReferenceAssetID = nil
			}
			j, e := f.jobs.CreateJob(c, user, input)
			if e != nil {
				old, lookup := f.reader.FindByIdempotencyKey(c, key, user)
				if lookup == nil {
					p.VideoJobID = &old.ID
					return pipelineChild(p, old, "save_video")
				}
				if !errors.Is(lookup, repo.ErrNotFound) {
					return lookup
				}
				return e
			}
			id, e := uuid.Parse(j.JobID)
			if e != nil {
				return e
			}
			p.VideoJobID = &id
			return nil
		}
		j, e := f.reader.FindByID(c, p.VideoJobID.String(), user)
		if e != nil {
			return e
		}
		return pipelineChild(p, j, "save_video")
	case "save_video":
		id, e := f.saveAsset(c, p, p.VideoJobID, "video", "วิดีโอจากตัวละคร")
		if e != nil {
			return f.markSaveFailure(p)
		}
		p.VideoAssetID = &id
		p.Stage = "done"
		p.Status = "succeeded"
	}
	return nil
}
func (f *PipelineFacade) markSaveFailure(p *models.StudioPipeline) error {
	code := "ASSET_SAVE_FAILED"
	message := "บันทึกเข้าคลังไม่สำเร็จ กดดำเนินการต่อเพื่อบันทึกอีกครั้ง"
	p.Status = "failed"
	p.FailureCode = &code
	p.FailureMessage = &message
	return nil
}
func pipelineConfirmedTerminal(j *models.VideoGenerationJob) bool {
	if j.Status != models.JobStatusFailed && j.Status != models.JobStatusCancelled {
		return false
	}
	if j.ProviderTaskID == nil || strings.TrimSpace(*j.ProviderTaskID) == "" || j.ProviderStatus == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(*j.ProviderStatus))
	return status == "failed" || status == "cancelled"
}
func pipelineRequiresNewRequest(code string) bool {
	return code == "PROVIDER_OUTPUT_REJECTED" || code == "PROVIDER_GENERATION_FAILED"
}
func pipelineChild(p *models.StudioPipeline, j *models.VideoGenerationJob, next string) error {
	switch j.Status {
	case models.JobStatusSucceeded:
		p.Stage = next
	case models.JobStatusFailed, models.JobStatusCancelled:
		code := strValOrDefault(j.FailureCode, "GENERATION_FAILED")
		message := "สร้างไม่สำเร็จ ภาพที่บันทึกแล้วจะยังอยู่ในคลัง"
		p.Status = "failed"

		if code == "WORKER_INTERRUPTED" || code == "SUBMISSION_INTERRUPTED" || code == "PROVIDER_TIMEOUT" || code == "PROVIDER_ERROR" {
			if pipelineConfirmedTerminal(j) {
				code = "PROVIDER_GENERATION_FAILED"
			} else {
				p.Status = "needs_attention"
				message = "ยังยืนยันผลการสร้างไม่ได้ กรุณาตรวจงานในคลังก่อนเริ่มคำขอใหม่"
			}
		}
		if code == "PROVIDER_OUTPUT_REJECTED" {
			message = "ผู้ให้บริการไม่อนุญาตวิดีโอที่สร้างตามนโยบายเนื้อหา กรุณาปรับฉากหรือภาพอ้างอิงแล้วสร้างคำขอใหม่"
		}
		if code == "PROVIDER_GENERATION_FAILED" {
			message = "ผู้ให้บริการยืนยันว่าการสร้างวิดีโอไม่สำเร็จ ภาพที่บันทึกแล้วจะยังอยู่ในคลัง"
		}

		p.FailureCode = &code
		p.FailureMessage = &message
	}
	return nil
}
func (f *PipelineFacade) saveAsset(c context.Context, p *models.StudioPipeline, jobID *uuid.UUID, kind, name string) (uuid.UUID, error) {
	if jobID == nil {
		return uuid.Nil, ErrAssetSource
	}
	j, e := f.reader.FindByID(c, jobID.String(), p.UserID.String())
	if e != nil {
		return uuid.Nil, e
	}
	expected := "image"
	if kind == "video" {
		expected = "video"
	}
	if j.Status != models.JobStatusSucceeded || j.OutputMediaKey == nil || !durableKey(*j.OutputMediaKey) || j.OutputMediaType == nil || *j.OutputMediaType != expected {
		return uuid.Nil, ErrAssetSource
	}
	id := pipelineID(p, "asset:"+kind)
	a := &models.LibraryAsset{Base: models.Base{ID: id}, UserID: p.UserID, Name: name, AssetType: kind, Prompt: j.Prompt, Scope: "personal", Version: 1, JobID: &j.ID, StorageKey: j.OutputMediaKey, MediaType: j.OutputMediaType}
	if e = f.store.InsertAsset(c, a); e != nil {
		return uuid.Nil, e
	}
	return a.ID, nil
}
