package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

var ErrDraftExpired = errors.New("draft task expired")

// ProviderSubmissionError carries only the sanitized diagnostic across the facade boundary.
type ProviderSubmissionError struct {
	code    string
	message string
}

func NewProviderSubmissionError(err error) *ProviderSubmissionError {
	code, message := modelark.VideoFailure(err)
	return &ProviderSubmissionError{code: code, message: message}
}

func (e *ProviderSubmissionError) Error() string   { return e.message }
func (e *ProviderSubmissionError) Unwrap() error   { return ErrProviderError }
func (e *ProviderSubmissionError) Code() string    { return e.code }
func (e *ProviderSubmissionError) Message() string { return e.message }

// JobFacade handles generation job creation, status, cancellation, and Continue Scene.
type JobFacade interface {
	CreateJob(ctx context.Context, userID string, in CreateJobInput) (*dto_open_api.CreateJobResponseDTO, error)
	GetJob(ctx context.Context, id, userID string) (*dto_open_api.JobDTO, error)
	ListJobs(ctx context.Context, userID string, page, limit int) (*dto_open_api.JobListDTO, error)
	CancelJob(ctx context.Context, id, userID string) (*dto_open_api.CancelJobResponseDTO, error)
	ContinueScene(ctx context.Context, sceneID, userID string, in ContinueSceneInput) (*dto_open_api.CreateJobResponseDTO, error)
	// PollAndUpdate is called by the background poller to advance job state.
	PollAndUpdate(ctx context.Context, job *models.VideoGenerationJob) error
	// BackfillOutputs copies outputs of succeeded jobs that still hold provider URLs into storage.
	BackfillOutputs(ctx context.Context, limit int) (int, error)
}

// CreateJobInput is the validated payload for job creation.
type CreateJobInput struct {
	pipelineRenderingVersion string
	draftSourceJobID         string
	PinnedModel              *modelark.ModelConfig
	Dialogue                 string
	ReferenceAssetID         *string
	Prompt                   string
	Mode                     string
	ModelID                  string
	AspectRatio              string
	DurationSeconds          int
	Quality                  string
	EnhancePrompt            bool
	UploadID                 *string
	SceneID                  *string
	CharacterProfileID       *string
	// CharacterProfileIDs takes precedence over CharacterProfileID. When both are empty
	// and SceneID is set, the scene's own characters are used.
	CharacterProfileIDs []string
	StyleProfileID      *string
	TagIDs              []string
	IdempotencyKey      *string

	// Draft requests preview-quality rendering when the resolved provider supports it.
	Draft bool

	// firstFrameKey is a storage key (or legacy provider URL) of a frame produced by an
	// earlier job. Unexported so only ContinueScene can set it, never request input.
	firstFrameKey string
}

// ContinueSceneInput carries per-scene overrides for a Continue Scene operation.
type ContinueSceneInput struct {
	Title           string
	ActionOverride  string
	CameraOverride  string
	SettingOverride string
	ModelID         string
	AspectRatio     string
	DurationSeconds int
	Quality         string
	UseLastFrame    bool
	IdempotencyKey  *string
}

type pipelineAssetReader interface {
	Asset(context.Context, string, string) (*models.LibraryAsset, error)
}
type jobFacade struct {
	pipelineAssets pipelineAssetReader
	jobs           repository_open_api.JobRepository
	uploads        repository_open_api.UploadRepository
	characters     repository_open_api.CharacterProfileRepository
	styles         repository_open_api.StyleProfileRepository
	scenes         repository_open_api.SceneRepository
	tags           repository_open_api.TagRepository
	ark            modelark.Client
	models         modelRegistry
	storage        mediaStorage
	fetcher        mediaFetcher
	maxActive      int
	production     *ProductionFacade
}

func NewJobFacade(
	jobs repository_open_api.JobRepository,
	uploads repository_open_api.UploadRepository,
	characters repository_open_api.CharacterProfileRepository,
	styles repository_open_api.StyleProfileRepository,
	scenes repository_open_api.SceneRepository,
	tags repository_open_api.TagRepository,
	ark modelark.Client,
	modelRegistry modelRegistry,
	storage mediaStorage,
	fetcher mediaFetcher,
	maxConcurrentJobsPerUser int,
) JobFacade {
	return &jobFacade{
		jobs:       jobs,
		uploads:    uploads,
		characters: characters,
		styles:     styles,
		scenes:     scenes,
		tags:       tags,
		ark:        ark,
		models:     modelRegistry,
		storage:    storage,
		fetcher:    fetcher,
		maxActive:  maxConcurrentJobsPerUser,
	}
}

func ConfigureJobPipeline(j JobFacade, a pipelineAssetReader) {
	if f, ok := j.(*jobFacade); ok {
		f.pipelineAssets = a
	}
}

func ConfigureJobProduction(j JobFacade, p *ProductionFacade) {
	if f, ok := j.(*jobFacade); ok {
		f.production = p
	}
}

func (f *jobFacade) CreateJob(ctx context.Context, userID string, in CreateJobInput) (*dto_open_api.CreateJobResponseDTO, error) {
	requestBytes, _ := json.Marshal(struct {
		Input                    CreateJobInput
		FirstFrame               string
		PipelineRenderingVersion string `json:",omitempty"`
		DraftSourceJobID         string `json:",omitempty"`
	}{in, in.firstFrameKey, in.pipelineRenderingVersion, in.draftSourceJobID})
	requestSum := sha256.Sum256(requestBytes)
	requestHash := hex.EncodeToString(requestSum[:])
	var scene *models.Scene
	if in.SceneID != nil {
		var e error
		scene, e = f.scenes.FindByID(ctx, *in.SceneID, userID)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if f.production != nil {
			if e = f.production.repo.ProjectForScene(ctx, scene, userID); e != nil {
				return nil, mapRepoErr(e)
			}
		}
	}
	// Idempotency check
	if in.IdempotencyKey != nil && *in.IdempotencyKey != "" {
		existing, err := f.jobs.FindByIdempotencyKey(ctx, *in.IdempotencyKey, userID)
		if err != nil && !errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, fmt.Errorf("check idempotency: %w", err)
		}
		if existing != nil {
			if existing.RequestHash != nil && *existing.RequestHash != requestHash {
				return nil, ErrConflict
			}
			if existing.Mode != in.Mode || existing.Prompt != in.Prompt || existing.ModelID != in.ModelID || existing.AspectRatio != in.AspectRatio || existing.DurationSeconds != in.DurationSeconds || existing.Quality != in.Quality || existing.EnhancePrompt != in.EnhancePrompt || !sameOptionalUUID(existing.SceneID, in.SceneID) || !sameOptionalUUID(existing.UploadID, in.UploadID) {
				return nil, ErrConflict
			}
			return mapCreateJobResponse(existing), nil
		}
	}

	// Rate limiting
	count, err := f.jobs.CountActiveByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count active jobs: %w", err)
	}
	if int(count) >= f.maxActive {
		return nil, ErrRateLimit
	}

	// Validate upload when required by mode
	refining := in.draftSourceJobID != ""
	var upload *models.Upload
	var libraryKey string
	var libraryID string
	if !refining && in.ReferenceAssetID != nil {
		if f.pipelineAssets == nil || in.UploadID != nil || in.SceneID != nil || in.firstFrameKey != "" || in.Mode != "image_image" {
			return nil, ErrValidation
		}
		a, e := f.pipelineAssets.Asset(ctx, *in.ReferenceAssetID, userID)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if e := validatePipelineCharacter(ctx, a, userID, f.jobs); e != nil {
			return nil, e
		}
		libraryKey = *a.StorageKey
		libraryID = a.ID.String()
	}
	if !refining && scene != nil && scene.ReferenceAssetID != nil && f.production != nil {
		a, e := f.production.repo.Asset(ctx, scene.ReferenceAssetID.String(), userID)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if a.StorageKey == nil || a.MediaType == nil || (*a.MediaType != "image" && !strings.HasPrefix(*a.MediaType, "image/")) {
			return nil, ErrInvalidFileType
		}
		if in.Mode == "text" {
			return nil, ErrImageRequired
		}
		libraryKey = *a.StorageKey
		libraryID = a.ID.String()
	}
	if !refining && (in.Mode == "image_video" || in.Mode == "image_image") && in.firstFrameKey == "" && libraryKey == "" {
		if in.UploadID == nil || *in.UploadID == "" {
			return nil, ErrImageRequired
		}
		upload, err = f.uploads.FindByID(ctx, *in.UploadID, userID)
		if err != nil {
			if errors.Is(err, repository_open_api.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("find upload: %w", err)
		}
		if time.Now().After(upload.ExpiresAt) {
			return nil, ErrImageExpired
		}
	}

	// Validate model
	modelCfg, ok := f.models.Lookup(in.ModelID)
	if in.PinnedModel != nil {
		modelCfg = *in.PinnedModel
		ok = modelCfg.InternalID == in.ModelID
	}
	if !ok {
		return nil, ErrModelInvalid
	}
	if in.Mode == "image_video" && !modelCfg.SupportsImgVideo {
		return nil, ErrModelInvalid
	}
	if in.Mode == "image_image" && !modelCfg.SupportsRefImage {
		return nil, ErrModelInvalid
	}
	if in.Dialogue != "" && !modelCfg.SupportsAudio {
		return nil, ErrModelInvalid
	}
	if !modelCfg.SupportsOutput(in.AspectRatio, in.DurationSeconds) {
		return nil, ErrModelInvalid
	}

	if e := ValidateVideoSettings(models.VideoSettings{Model: in.ModelID, Mode: in.Mode, AspectRatio: in.AspectRatio, Duration: in.DurationSeconds, Quality: in.Quality}, modelCfg); e != nil {
		return nil, e
	}
	// Account overrides can change the provider behind an internal model.
	if in.Draft && !modelCfg.SupportsDraft() {
		return nil, ErrValidation
	}

	var compilerIn promptcompiler.Inputs
	var compiled *promptcompiler.Result
	var snapJSON []byte
	if refining {
		source, e := f.jobs.FindByID(ctx, in.draftSourceJobID, userID)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if source.UserID.String() != userID || !source.IsDraft || source.Status != models.JobStatusSucceeded || source.ProviderTaskID == nil || *source.ProviderTaskID == "" || in.Draft || !modelCfg.SupportsDraft() || in.UploadID != nil || in.ReferenceAssetID != nil || in.SceneID != nil || in.firstFrameKey != "" || in.Quality != "high" {
			return nil, ErrValidation
		}
		if !time.Now().Before(modelark.DraftExpiresAt(source.CreatedAt)) {
			return nil, ErrDraftExpired
		}
		var snapshot struct {
			ProviderModelID string `json:"providerModelId"`
		}
		if json.Unmarshal(source.EffectiveInputs, &snapshot) != nil || snapshot.ProviderModelID != modelCfg.ProviderModelID || source.ModelID != in.ModelID || source.Mode != in.Mode || source.Prompt != in.Prompt || source.AspectRatio != in.AspectRatio || source.DurationSeconds != in.DurationSeconds {
			return nil, ErrValidation
		}
		compiled = &promptcompiler.Result{ProviderModelID: modelCfg.ProviderModelID, Resolution: "1080p", ContentItems: []modelark.ContentItem{{Type: modelark.ContentTypeDraftTask, DraftTaskID: *source.ProviderTaskID}}}
		if source.CompiledPrompt != nil {
			compiled.CompiledPrompt = *source.CompiledPrompt
		}
		var frozen map[string]json.RawMessage
		if json.Unmarshal(source.EffectiveInputs, &frozen) != nil {
			return nil, ErrValidation
		}
		frozen["sourceDraftJobId"], _ = json.Marshal(source.ID.String())
		frozen["resolution"], _ = json.Marshal("1080p")
		snapJSON, err = json.Marshal(frozen)
	} else {
		compilerIn, err = f.buildCompilerInputs(ctx, userID, in, upload)
		if err != nil {
			return nil, err
		}
		if libraryKey != "" {
			role := "first_frame"
			if in.Mode == "image_image" {
				role = "reference"
			}
			compilerIn.Assets = append(compilerIn.Assets, promptcompiler.AssetRef{UploadID: libraryID, StorageKey: libraryKey, Role: role, MimeType: "image/png"})
		}
		compiled, err = promptcompiler.Compile(compilerIn, modelCfg)
		if err != nil {
			return nil, fmt.Errorf("compile prompt: %w", err)
		}
		// BytePlus downloads reference media itself, so it needs a URL it can reach,
		// not our private storage key.
		for i := range compiled.ContentItems {
			item := &compiled.ContentItems[i]
			if item.URL == "" {
				continue
			}
			signed, _, err := f.resolveMediaURL(ctx, item.URL)
			if err != nil {
				return nil, fmt.Errorf("sign reference media: %w", err)
			}
			item.URL = signed
		}

		if in.Draft {
			compiled.Resolution = "480p"
			compiled.Snapshot.Resolution = "480p"
		}
		snapJSON, err = json.Marshal(struct {
			promptcompiler.EffectiveInputsSnapshot
			ProviderModelID string `json:"providerModelId"`
		}{compiled.Snapshot, compiled.ProviderModelID})
	}
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}

	// Persist job in queued state
	uid, _ := uuid.Parse(userID)
	job := &models.VideoGenerationJob{
		UserID:          uid,
		Status:          models.JobStatusQueued,
		Prompt:          in.Prompt,
		Mode:            in.Mode,
		ModelID:         in.ModelID,
		AspectRatio:     in.AspectRatio,
		DurationSeconds: in.DurationSeconds,
		Quality:         in.Quality,
		EnhancePrompt:   in.EnhancePrompt,
		IsDraft:         in.Draft,
		CompiledPrompt:  &compiled.CompiledPrompt,
		IdempotencyKey:  in.IdempotencyKey,
		RequestHash:     &requestHash,
	}
	job.EffectiveInputs = snapJSON

	if upload != nil {
		job.UploadID = &upload.ID
	}
	if in.SceneID != nil {
		sceneUUID, err := uuid.Parse(*in.SceneID)
		if err == nil {
			job.SceneID = &sceneUUID
		}
	}
	if len(compilerIn.Characters) > 0 {
		if cpUUID, err := uuid.Parse(compilerIn.Characters[0].ProfileID); err == nil {
			job.CharacterProfileID = &cpUUID
		}
	}
	if in.StyleProfileID != nil {
		spUUID, err := uuid.Parse(*in.StyleProfileID)
		if err == nil {
			job.StyleProfileID = &spUUID
		}
	}

	if f.production != nil {
		stored, created, e := f.production.repo.EnqueueVideo(ctx, job, f.maxActive)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if !created {
			if stored.RequestHash == nil || *stored.RequestHash != requestHash {
				return nil, ErrConflict
			}
			return mapCreateJobResponse(stored), nil
		}
	} else if err := f.jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("persist job: %w", err)
	}

	// Submit to provider
	arkReq := modelark.CreateTaskRequest{
		Model:               compiled.ProviderModelID,
		Content:             compiled.ContentItems,
		Ratio:               in.AspectRatio,
		DurationSeconds:     int64(in.DurationSeconds),
		Resolution:          compiled.Resolution,
		OptimizePrompt:      in.EnhancePrompt,
		GenerateAudio:       compiled.GenerateAudio,
		Watermark:           false,
		ReturnLastFrame:     true,
		ExpiresAfterSeconds: 3600,
		// Draft eligibility follows the pinned provider.
		Draft: in.Draft && modelCfg.SupportsDraft(),
	}

	if refining {
		arkReq = modelark.CreateTaskRequest{Model: compiled.ProviderModelID, Content: compiled.ContentItems, Resolution: "1080p", ReturnLastFrame: true, ExpiresAfterSeconds: 3600}
	}

	log.Printf("[ark] submit job=%s model=%s", job.ID, arkReq.Model)

	providerTaskID, err := f.ark.CreateTask(modelark.WithUsageOwner(ctx, job.UserID, job.ID), arkReq)
	if err != nil {
		failure := NewProviderSubmissionError(err)
		code, message := failure.Code(), failure.Message()
		diagnostic := modelark.VideoFailureDiagnostic(err)
		var texts, images, videos, firstFrames, lastFrames, references, otherRoles int
		for _, item := range arkReq.Content {
			switch item.Type {
			case modelark.ContentTypeText:
				texts++
			case modelark.ContentTypeImageURL:
				images++
			case modelark.ContentTypeVideoURL:
				videos++
			}
			if item.Type == modelark.ContentTypeText {
				continue
			}
			switch item.Role {
			case modelark.RoleFirstFrame:
				firstFrames++
			case modelark.RoleLastFrame:
				lastFrames++
			case modelark.RoleReferenceImage:
				references++
			default:
				otherRoles++
			}
		}
		log.Printf("[ark] create task failed job=%s code=%s provider_status=%d provider_code=%q param=%q model=%s resolution=%s duration=%d ratio=%s audio=%t optimize_prompt=%t text_count=%d image_count=%d video_count=%d first_frame_count=%d last_frame_count=%d reference_count=%d other_role_count=%d", job.ID, code, diagnostic.HTTPStatus, diagnostic.ProviderCode, diagnostic.Param, arkReq.Model, arkReq.Resolution, arkReq.DurationSeconds, arkReq.Ratio, arkReq.GenerateAudio, arkReq.OptimizePrompt, texts, images, videos, firstFrames, lastFrames, references, otherRoles)
		now := time.Now()
		_ = f.jobs.UpdateStatus(ctx, job.ID.String(), models.JobStatusFailed, map[string]any{
			"failure_code":    code,
			"failure_message": message,
			"completed_at":    now,
		})
		return nil, failure
	}
	log.Printf("[ark] task created job=%s providerTaskID=%s", job.ID, providerTaskID)

	// Update job with provider task ID → submitted
	now := time.Now()
	// Once accepted, caller cancellation must not discard the only provider task ID.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		err = f.jobs.UpdateStatus(persistCtx, job.ID.String(), models.JobStatusSubmitted, map[string]any{
			"provider_task_id": providerTaskID,
			"submitted_at":     now,
		})
		if err == nil || errors.Is(err, repository_open_api.ErrNotFound) || errors.Is(err, repository_open_api.ErrConflict) {
			break
		}
		if attempt < 2 {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-persistCtx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
		if persistCtx.Err() != nil {
			break
		}
	}
	if err != nil {
		// Keep the durable identity so a replay cannot create another billed task.
		// The poller marks an unconfirmed submission explicitly; it never resubmits.
		log.Printf("[ark] accepted task persistence unconfirmed job=%s", job.ID)
		return mapCreateJobResponse(job), nil
	}
	job.Status = models.JobStatusSubmitted

	return mapCreateJobResponse(job), nil
}

func (f *jobFacade) GetJob(ctx context.Context, id, userID string) (*dto_open_api.JobDTO, error) {
	job, err := f.jobs.FindByID(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get job: %w", err)
	}
	dto := mapJobDTO(job)
	f.attachResult(ctx, job, dto)
	f.applyProgress(ctx, job, dto)
	return dto, nil
}

func (f *jobFacade) ListJobs(ctx context.Context, userID string, page, limit int) (*dto_open_api.JobListDTO, error) {
	jobs, total, err := f.jobs.List(ctx, userID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	items := make([]dto_open_api.JobDTO, len(jobs))
	for i, j := range jobs {
		jCopy := j
		dto := mapJobDTO(&jCopy)
		f.attachResult(ctx, &jCopy, dto)
		f.applyProgress(ctx, &jCopy, dto)
		items[i] = *dto
	}
	return &dto_open_api.JobListDTO{
		Items: items,
		Total: int(total),
		Page:  page,
		Limit: limit,
	}, nil
}

func (f *jobFacade) CancelJob(ctx context.Context, id, userID string) (*dto_open_api.CancelJobResponseDTO, error) {
	job, err := f.jobs.FindByID(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find job: %w", err)
	}

	if job.Mode == "text_image" && job.Status != models.JobStatusQueued {
		return nil, ErrConflict
	}
	if job.Status.IsTerminal() {
		return nil, ErrConflict
	}

	// Attempt provider-level cancel only when task has been submitted and is still queued
	if job.ProviderTaskID != nil && (job.Status == models.JobStatusSubmitted) {
		_ = f.ark.CancelTask(modelark.WithUsageOwner(ctx, job.UserID, job.ID), *job.ProviderTaskID)
	}

	now := time.Now()
	if err := f.jobs.UpdateStatus(ctx, id, models.JobStatusCancelled, map[string]any{
		"completed_at": now,
	}); err != nil {
		if errors.Is(err, repository_open_api.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("cancel job: %w", err)
	}

	return &dto_open_api.CancelJobResponseDTO{
		JobID:  id,
		Status: string(models.JobStatusCancelled),
	}, nil
}

// ContinueScene creates a new scene that continues from a previous scene,
// then submits a generation job for it.
func (f *jobFacade) ContinueScene(ctx context.Context, sceneID, userID string, in ContinueSceneInput) (*dto_open_api.CreateJobResponseDTO, error) {
	prev, err := f.scenes.FindByID(ctx, sceneID, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find previous scene: %w", err)
	}

	uid, _ := uuid.Parse(userID)
	prevID := prev.ID
	newScene := &models.Scene{
		UserID:             uid,
		Title:              strPtr(in.Title),
		CharacterProfileID: prev.CharacterProfileID,
		StyleProfileID:     prev.StyleProfileID,
		PreviousSceneID:    &prevID,
		SceneOrder:         prev.SceneOrder + 1,
		ActionOverride:     strPtr(in.ActionOverride),
		CameraOverride:     strPtr(in.CameraOverride),
		SettingOverride:    strPtr(in.SettingOverride),
	}

	if err := f.scenes.Create(ctx, newScene); err != nil {
		return nil, fmt.Errorf("create continuation scene: %w", err)
	}
	if ids := sceneCharacterIDs(prev); len(ids) > 0 {
		if err := f.scenes.SetCharacters(ctx, newScene.ID, ids); err != nil {
			return nil, fmt.Errorf("copy scene characters: %w", err)
		}
	}

	// Inherit tags from previous scene
	if len(prev.Tags) > 0 {
		tagIDs := make([]uuid.UUID, len(prev.Tags))
		for i, t := range prev.Tags {
			tagIDs[i] = t.ID
		}
		_ = f.scenes.SetTags(ctx, newScene.ID, tagIDs)
	}

	var firstFrameKey string
	mode := "text"
	if in.UseLastFrame && prev.SelectedJobID != nil {
		prevJob, err := f.jobs.FindByID(ctx, prev.SelectedJobID.String(), userID)
		if err == nil && prevJob.Status == models.JobStatusSucceeded && prevJob.OutputThumbKey != nil {
			firstFrameKey = *prevJob.OutputThumbKey
			mode = "image_video"
		}
	}

	sceneIDStr := newScene.ID.String()
	jobIn := CreateJobInput{
		Prompt:          in.ActionOverride,
		Mode:            mode,
		ModelID:         in.ModelID,
		AspectRatio:     in.AspectRatio,
		DurationSeconds: in.DurationSeconds,
		Quality:         in.Quality,
		SceneID:         &sceneIDStr,
		IdempotencyKey:  in.IdempotencyKey,
	}
	if newScene.StyleProfileID != nil {
		v := newScene.StyleProfileID.String()
		jobIn.StyleProfileID = &v
	}

	jobIn.firstFrameKey = firstFrameKey

	return f.CreateJob(ctx, userID, jobIn)
}

// PollAndUpdate is called by the background poller to advance job state.
func (f *jobFacade) PollAndUpdate(ctx context.Context, job *models.VideoGenerationJob) error {
	if job.ProviderTaskID == nil {
		return nil
	}

	result, err := f.ark.GetTask(modelark.WithUsageOwner(ctx, job.UserID, job.ID), *job.ProviderTaskID)
	if err != nil {
		return fmt.Errorf("poll job %s: %w", job.ID, err)
	}

	_ = f.jobs.UpdatePolled(ctx, job.ID.String(), time.Now())

	appStatus := modelark.MapProviderStatus(result.Status)
	if appStatus == job.Status {
		return nil
	}

	updates := map[string]any{
		"provider_status": string(result.Status),
	}

	if appStatus.IsTerminal() {
		now := time.Now()
		updates["completed_at"] = now
	}

	switch appStatus {
	case models.JobStatusSucceeded:
		if err := f.jobs.UpdateCurrentStep(ctx, job.ID.String(), StepSaving); err != nil {
			log.Printf("[progress] job=%s mark saving: %v", job.ID, err)
		}
		mediaKey, thumbKey, err := f.persistOutputs(ctx, job, result.VideoURL, result.LastFrameURL)
		if err != nil {
			if errors.Is(err, errInvalidVideoOutput) {
				appStatus = models.JobStatusFailed
				updates["failure_code"] = "INVALID_OUTPUT"
				updates["failure_message"] = "ผู้ให้บริการส่งไฟล์วิดีโอที่ไม่สมบูรณ์ โปรดลองสร้างใหม่"
				break
			}
			if job.SubmittedAt != nil && time.Since(*job.SubmittedAt) > providerOutputLifetime {
				log.Printf("[storage] job=%s output expired before it could be stored: %v", job.ID, err)
				appStatus = models.JobStatusFailed
				updates["failure_code"] = "OUTPUT_EXPIRED"
				updates["failure_message"] = "Generated video could not be saved"
				break
			}
			// Leaving the job in processing makes the poller retry on its next tick.
			return fmt.Errorf("persist outputs for job %s: %w", job.ID, err)
		}
		updates["output_duration_ms"] = job.OutputDurationMs
		updates["output_media_key"] = mediaKey
		updates["output_thumb_key"] = thumbKey
		updates["output_media_type"] = "video"
		updates["current_step"] = StepDone
	case models.JobStatusFailed:
		updates["failure_code"] = result.ErrorCode
		updates["failure_message"] = result.ErrorMessage // sanitized provider message
		diagnostic, e := json.Marshal(modelark.GenerationFailureDetails(result.FailureDiagnostic.ProviderCode))
		if e != nil {
			return fmt.Errorf("marshal generation failure diagnostic: %w", e)
		}
		updates["provider_error"] = datatypes.JSON(diagnostic)
	}

	return f.jobs.UpdateStatus(ctx, job.ID.String(), appStatus, updates)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (f *jobFacade) buildCompilerInputs(ctx context.Context, userID string, in CreateJobInput, upload *models.Upload) (promptcompiler.Inputs, error) {
	inputs := promptcompiler.Inputs{
		PipelineRenderingVersion: in.pipelineRenderingVersion,
		Dialogue:                 in.Dialogue,
		ModelID:                  in.ModelID,
		Quality:                  in.Quality,
	}

	var scene *models.Scene
	if in.SceneID != nil {
		if sc, err := f.scenes.FindByID(ctx, *in.SceneID, userID); err == nil {
			scene = sc
		}
	}

	characters, err := f.resolveJobCharacters(ctx, userID, in, scene)
	if err != nil {
		return inputs, err
	}
	for _, cp := range characters {
		inputs.Characters = append(inputs.Characters, promptcompiler.CharacterInputs{
			ProfileID:        cp.ID.String(),
			ProfileVersion:   cp.Version,
			Name:             cp.Name,
			Appearance:       strVal(cp.Appearance),
			Hairstyle:        strVal(cp.Hairstyle),
			Clothing:         strVal(cp.Clothing),
			Accessories:      strVal(cp.Accessories),
			DefiningFeatures: strVal(cp.DefiningFeatures),
			ConsistencyNotes: strVal(cp.ConsistencyNotes),
		})
	}

	if in.StyleProfileID != nil {
		sp, err := f.styles.FindByID(ctx, *in.StyleProfileID, userID)
		if err == nil {
			inputs.Style = &promptcompiler.StyleInputs{
				ProfileID:      sp.ID.String(),
				Name:           sp.Name,
				VisualStyle:    strVal(sp.VisualStyle),
				ColorPalette:   strVal(sp.ColorPalette),
				Lighting:       strVal(sp.Lighting),
				Mood:           strVal(sp.Mood),
				CameraLanguage: strVal(sp.CameraLanguage),
				QualityNotes:   strVal(sp.QualityNotes),
			}
		}
	}

	if scene != nil {
		desc := strVal(scene.Description)
		// If the scene has no description of its own, use the request prompt
		if desc == "" && strVal(scene.ActionOverride) == "" {
			desc = in.Prompt
		}
		inputs.Scene = &promptcompiler.SceneInputs{
			SceneID:         scene.ID.String(),
			Description:     desc,
			ActionOverride:  strVal(scene.ActionOverride),
			CameraOverride:  strVal(scene.CameraOverride),
			SettingOverride: strVal(scene.SettingOverride),
		}
	}

	// Reference asset from upload
	if upload != nil {
		role := "first_frame"
		if in.Mode == "image_image" {
			role = "reference"
		}
		inputs.Assets = append(inputs.Assets, promptcompiler.AssetRef{
			UploadID:   upload.ID.String(),
			StorageKey: upload.StorageKey,
			Role:       role,
			MimeType:   upload.MimeType,
		})
	}

	if upload == nil && in.firstFrameKey != "" {
		inputs.Assets = append(inputs.Assets, promptcompiler.AssetRef{
			StorageKey: in.firstFrameKey,
			Role:       "first_frame",
			MimeType:   frameMimeType(in.firstFrameKey),
		})
	}

	// Fallback: no scene loaded at all — use raw prompt directly
	if inputs.Scene == nil && in.Prompt != "" {
		inputs.Scene = &promptcompiler.SceneInputs{
			Description: in.Prompt,
		}
	}

	return inputs, nil
}

func mapCreateJobResponse(job *models.VideoGenerationJob) *dto_open_api.CreateJobResponseDTO {
	return &dto_open_api.CreateJobResponseDTO{
		JobID:            job.ID.String(),
		Status:           string(job.Status),
		EstimatedCredits: job.EstimatedCredits,
		CreatedAt:        job.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func mapJobDTO(job *models.VideoGenerationJob) *dto_open_api.JobDTO {
	dto := &dto_open_api.JobDTO{
		OutputDurationMs: job.OutputDurationMs,
		JobID:            job.ID.String(),
		Status:           string(job.Status),
		Progress:         job.Progress,
		CurrentStep:      job.CurrentStep,
		Model:            job.ModelID,
		AspectRatio:      job.AspectRatio,
		Duration:         job.DurationSeconds,
		Quality:          job.Quality,
		CreditsCharged:   job.ChargedCredits,
		CompiledPrompt:   job.CompiledPrompt,
		IsDraft:          job.IsDraft,
		CreatedAt:        job.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        job.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if job.Mode == "text_image" {
		dto.RawPrompt = job.Prompt
		dto.ImageCategory = "character"
		var snapshot struct {
			Category        string                         `json:"category"`
			ProductDesign   *dto_open_api.ProductDesignDTO `json:"productDesign"`
			CharacterDesign *characterdesign.Options       `json:"characterDesign"`
		}
		if json.Unmarshal(job.EffectiveInputs, &snapshot) == nil {
			if snapshot.Category == "product" {
				dto.ImageCategory = "product"
				dto.ProductDesign = snapshot.ProductDesign
			} else if snapshot.CharacterDesign != nil {
				if options, err := characterdesign.Normalize(*snapshot.CharacterDesign); err == nil {
					dto.CharacterDesign = &dto_open_api.CharacterDesignDTO{VisualStyle: options.VisualStyle, Mood: options.Mood, DesignFormat: options.DesignFormat}
				}
			}
		}
	}
	if job.SceneID != nil {
		v := job.SceneID.String()
		dto.SceneID = &v
	}
	if job.CharacterProfileID != nil {
		v := job.CharacterProfileID.String()
		dto.CharacterProfileID = &v
	}
	if job.StyleProfileID != nil {
		v := job.StyleProfileID.String()
		dto.StyleProfileID = &v
	}
	if job.CompletedAt != nil {
		s := job.CompletedAt.UTC().Format(time.RFC3339)
		dto.CompletedAt = &s
	}
	if job.Status == models.JobStatusFailed {
		dto.Error = &dto_open_api.JobError{
			Code:    strValOrDefault(job.FailureCode, "UNKNOWN"),
			Message: strValOrDefault(job.FailureMessage, "Generation failed"),
		}
	}
	return dto
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func strValOrDefault(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// resolveJobCharacters uses explicitly requested characters, which must all belong to
// the user, or else the scene's characters, skipping any profile deleted since.
func (f *jobFacade) resolveJobCharacters(ctx context.Context, userID string, in CreateJobInput, scene *models.Scene) ([]models.CharacterProfile, error) {
	if requested := requestedCharacterIDs(in.CharacterProfileIDs, in.CharacterProfileID); len(requested) > 0 {
		ids, err := parseCharacterIDs(requested)
		if err != nil {
			return nil, err
		}
		return loadOwnedCharacters(ctx, f.characters, ids, userID)
	}
	if scene == nil || len(scene.Characters) == 0 {
		return nil, nil
	}
	profiles, err := f.characters.FindByIDs(ctx, sceneCharacterIDs(scene), userID)
	if err != nil {
		return nil, fmt.Errorf("load scene characters: %w", err)
	}
	return profiles, nil
}

func sameOptionalUUID(a *uuid.UUID, b *string) bool {
	if a == nil {
		return b == nil || *b == ""
	}
	return b != nil && a.String() == *b
}

// The same provenance boundary applies before pipeline acceptance and video submission.
func validatePipelineCharacter(ctx context.Context, a *models.LibraryAsset, userID string, jobs pipelineJobReader) error {
	if a.UserID.String() != userID || a.JobID == nil || a.UploadID != nil || a.AssetType != "character" || a.StorageKey == nil || !durableKey(*a.StorageKey) || a.MediaType == nil || *a.MediaType != "image" {
		return ErrAssetSource
	}
	j, e := jobs.FindByID(ctx, a.JobID.String(), userID)
	if e != nil {
		return mapRepoErr(e)
	}
	var snap struct {
		Category string `json:"category"`
	}
	if j.UserID.String() != userID || j.Status != models.JobStatusSucceeded || j.Mode != "text_image" || j.OutputMediaKey == nil || *j.OutputMediaKey != *a.StorageKey || json.Unmarshal(j.EffectiveInputs, &snap) != nil || snap.Category != "character" {
		return ErrAssetSource
	}
	return nil
}
