package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/productdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAssetSource = errors.New("asset source not ready")
var ErrCharacterReferenceUnavailable = errors.New("character reference analysis unavailable")
var ErrImageUnavailable = errors.New("image generation unavailable")
var ErrIdempotencyMismatch = errors.New("idempotency key reused for different input")

type StudioFacade struct {
	repo                   repo.StudioRepository
	jobs                   repo.JobRepository
	uploads                repo.UploadRepository
	storage                mediaStorage
	secret                 string
	expiry                 time.Duration
	imageModel             string
	maxJobs                int64
	referenceAnalysisModel string
}

func NewStudioFacade(r repo.StudioRepository, j repo.JobRepository, u repo.UploadRepository, s mediaStorage, secret string, expiry time.Duration, imageModel string, max int) *StudioFacade {
	return &StudioFacade{repo: r, jobs: j, uploads: u, storage: s, secret: secret, expiry: expiry, imageModel: imageModel, maxJobs: int64(max)}
}
func (f *StudioFacade) WithReferenceAnalysisModel(model string) *StudioFacade {
	f.referenceAnalysisModel = strings.TrimSpace(model)
	return f
}
func UserView(u *models.User) dto_open_api.UserDTO {
	return dto_open_api.UserDTO{ID: u.ID.String(), Email: u.Email, DisplayName: u.DisplayName, Plan: u.Plan, Role: u.Role}
}
func (f *StudioFacade) Authenticate(c context.Context, r request_open_api.AuthRequest, register bool) (*dto_open_api.AuthDTO, error) {
	email := strings.ToLower(strings.TrimSpace(r.Email))
	var u *models.User
	if register {
		hash, e := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
		if e != nil {
			return nil, ErrValidation
		}
		s := string(hash)
		u = &models.User{Email: email, DisplayName: strings.TrimSpace(r.DisplayName), PasswordHash: &s, Plan: "free", Role: "user"}
		if e = f.repo.CreateUser(c, u); e != nil {
			if errors.Is(e, repo.ErrConflict) {
				return nil, ErrConflict
			}
			return nil, e
		}
	} else {
		var e error
		u, e = f.repo.UserByEmail(c, email)
		if e != nil {
			if errors.Is(e, repo.ErrNotFound) {
				return nil, ErrUnauthorized
			}
			return nil, e
		}
		if u.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(r.Password)) != nil {
			return nil, ErrUnauthorized
		}
	}
	now := time.Now()
	exp := now.Add(f.expiry)
	claims := jwt.MapClaims{"sub": u.ID.String(), "plan": u.Plan, "role": u.Role, "iat": now.Unix(), "exp": exp.Unix()}
	token, e := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(f.secret))
	if e != nil {
		return nil, e
	}
	return &dto_open_api.AuthDTO{AccessToken: token, ExpiresAt: exp.UTC().Format(time.RFC3339), User: UserView(u)}, nil
}
func (f *StudioFacade) Me(c context.Context, user string) (*dto_open_api.UserDTO, error) {
	u, e := f.repo.UserByID(c, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	v := UserView(u)
	return &v, nil
}
func (f *StudioFacade) Capabilities() dto_open_api.ImageCapabilitiesDTO {
	d := dto_open_api.ImageCapabilitiesDTO{Categories: []string{"character", "product"}, Enabled: f.imageModel != "", Model: "character-image", AspectRatios: []string{"1:1", "3:4", "4:3", "16:9", "9:16"}, SupportsReferenceImage: f.imageModel != ""}
	d.CharacterReferenceMode = "image_to_image"
	d.SupportsCharacterReferenceImage = f.imageModel != ""
	if !d.SupportsCharacterReferenceImage {
		reason := "ยังไม่พร้อมสร้างภาพจากภาพอ้างอิงตัวละคร กรุณาติดต่อผู้ดูแลระบบหรือลองสร้างจากข้อความอย่างเดียว"
		d.CharacterReferenceUnavailableReason = &reason
	}
	if f.imageModel == "" {
		s := "ยังไม่พร้อมสร้างภาพ กรุณาติดต่อผู้ดูแลระบบเพื่อตั้งค่าบริการและเปิดใช้งานโมเดล Seedream"
		d.UnavailableReason = &s
	}
	return d
}
func (f *StudioFacade) CreateImage(c context.Context, user string, in request_open_api.CreateImageRequest) (*dto_open_api.CreateJobResponseDTO, error) {
	return f.createImage(c, user, in, "")
}
func (f *StudioFacade) createImage(c context.Context, user string, in request_open_api.CreateImageRequest, renderingVersion string) (*dto_open_api.CreateJobResponseDTO, error) {
	category := in.Category
	if category == "" {
		category = "character"
	}
	if category != "character" && category != "product" || category == "product" && in.DesignFormat != "" {
		return nil, ErrValidation
	}
	var options characterdesign.Options
	var productOptions productdesign.Options
	var optionErr error
	if category == "product" {
		productOptions, optionErr = productdesign.Normalize(productdesign.Options{VisualStyle: in.VisualStyle, Mood: in.Mood})
	} else {
		options, optionErr = characterdesign.Normalize(characterdesign.Options{VisualStyle: in.VisualStyle, Mood: in.Mood, DesignFormat: in.DesignFormat})
	}
	if optionErr != nil || strings.TrimSpace(in.Prompt) == "" || utf8.RuneCountInString(in.Prompt) > 2000 || ImageSize(in.AspectRatio) == "" {
		return nil, ErrValidation
	}
	uid, e := uuid.Parse(user)
	if e != nil {
		return nil, ErrUnauthorized
	}
	var requestedUploadID *uuid.UUID
	if in.ReferenceImageID != nil {
		parsed, err := uuid.Parse(*in.ReferenceImageID)
		if err != nil {
			return nil, ErrValidation
		}
		requestedUploadID = &parsed
	}
	// Replays return the original immutable job even after configuration or upload expiry changes.
	if f.jobs != nil {
		old, err := f.jobs.FindByIdempotencyKey(c, in.IdempotencyKey, user)
		if err == nil {
			candidate := &models.VideoGenerationJob{Mode: "text_image", Prompt: strings.TrimSpace(in.Prompt), AspectRatio: in.AspectRatio}
			if err := matchImageReplay(old, candidate, category, options, productOptions, requestedUploadID); err != nil {
				return nil, err
			}
			return mapCreateJobResponse(old), nil
		}
		if !errors.Is(err, repo.ErrNotFound) {
			return nil, err
		}
	}
	if f.imageModel == "" {
		return nil, ErrImageUnavailable
	}
	var uploadID *uuid.UUID
	if in.ReferenceImageID != nil {
		u, e := f.uploads.FindByID(c, *in.ReferenceImageID, user)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if u.ExpiresAt.Before(time.Now()) {
			return nil, ErrImageExpired
		}
		if !strings.HasPrefix(u.MimeType, "image/") {
			return nil, ErrValidation
		}
		uploadID = &u.ID
	}
	key := in.IdempotencyKey
	var snapshot []byte
	var prompt string
	if category == "product" {
		snapshot, _ = json.Marshal(map[string]any{"provider": "byteplus", "model": f.imageModel, "size": ImageSize(in.AspectRatio), "category": category, "productDesign": productOptions, "compilerVersion": productdesign.CompilerVersion, "imageGenerationMode": func() string {
			if uploadID != nil {
				return "image_to_image"
			}
			return "text_to_image"
		}()})
		prompt, e = productdesign.Compile(in.Prompt, productOptions, in.ReferenceImageID != nil)
	} else {
		mode := "text_to_image"
		if uploadID != nil {
			mode = "image_to_image"
		}
		compilerVersion := characterdesign.CompilerVersion
		if renderingVersion == characterdesign.PipelineStylized3DCompilerVersion || renderingVersion == promptcompiler.PipelineCinematicVideoVersion || renderingVersion == promptcompiler.PipelineFilmedHumanVideoVersion {
			compilerVersion = characterdesign.PipelineStylized3DCompilerVersion
			prompt, e = characterdesign.CompilePipelineStylized3D(in.Prompt, options, uploadID != nil)
		} else if renderingVersion == characterdesign.PipelinePolished3DCompilerVersion {
			compilerVersion = renderingVersion
			prompt, e = characterdesign.CompilePipelinePolished3D(in.Prompt, options, uploadID != nil)
		} else if renderingVersion == characterdesign.PipelineSemiRealisticCompilerVersion {
			compilerVersion = renderingVersion
			prompt, e = characterdesign.CompilePipelineSemiRealistic(in.Prompt, options, uploadID != nil)
		} else if renderingVersion == characterdesign.PipelineSoftRealisticCompilerVersion {
			compilerVersion = renderingVersion
			prompt, e = characterdesign.CompilePipelineSoftRealistic(in.Prompt, options, uploadID != nil)
		} else if renderingVersion == characterdesign.PipelineRealisticCompilerVersion {
			compilerVersion = renderingVersion
			prompt, e = characterdesign.CompilePipelineRealistic(in.Prompt, options, uploadID != nil)
		} else if renderingVersion == characterdesign.PipelineCompilerVersion {
			compilerVersion = renderingVersion
			prompt, e = characterdesign.CompilePipeline(in.Prompt, options, uploadID != nil)
		} else {
			prompt, e = characterdesign.CompileSynthetic(in.Prompt, options, uploadID != nil)
		}
		snapshot, _ = json.Marshal(map[string]any{"provider": "byteplus", "model": f.imageModel, "size": ImageSize(in.AspectRatio), "category": category, "characterDesign": options, "compilerVersion": compilerVersion, "imageGenerationMode": mode})
	}
	if e != nil {
		return nil, ErrValidation
	}
	j := &models.VideoGenerationJob{CompiledPrompt: &prompt, EffectiveInputs: snapshot, UserID: uid, Status: models.JobStatusQueued, Prompt: strings.TrimSpace(in.Prompt), Mode: "text_image", ModelID: "character-image", AspectRatio: in.AspectRatio, DurationSeconds: 0, Quality: "standard", IdempotencyKey: &key, UploadID: uploadID}
	out, e := f.repo.EnqueueImage(c, j, f.maxJobs)
	if e != nil {
		if errors.Is(e, repo.ErrConflict) {
			return nil, ErrRateLimit
		}
		return nil, e
	}
	if e := matchImageReplay(out, j, category, options, productOptions, uploadID); e != nil {
		return nil, e
	}
	return mapCreateJobResponse(out), nil
}
func matchImageReplay(out, j *models.VideoGenerationJob, category string, options characterdesign.Options, productOptions productdesign.Options, uploadID *uuid.UUID) error {
	sameUpload := (out.UploadID == nil && uploadID == nil) || (out.UploadID != nil && uploadID != nil && *out.UploadID == *uploadID)
	var existingSnapshot struct {
		Category        string                  `json:"category"`
		CharacterDesign characterdesign.Options `json:"characterDesign"`
		ProductDesign   productdesign.Options   `json:"productDesign"`
	}
	if e := json.Unmarshal(out.EffectiveInputs, &existingSnapshot); e != nil {
		return ErrIdempotencyMismatch
	}
	if existingSnapshot.Category == "" {
		existingSnapshot.Category = "character"
	}
	sameOptions := false
	if category == "product" {
		existingOptions, optionsErr := productdesign.Normalize(existingSnapshot.ProductDesign)
		sameOptions = optionsErr == nil && existingOptions == productOptions
	} else {
		existingOptions, optionsErr := characterdesign.Normalize(existingSnapshot.CharacterDesign)
		sameOptions = optionsErr == nil && existingOptions == options
	}
	if existingSnapshot.Category != category || !sameOptions || out.Mode != j.Mode || out.Prompt != j.Prompt || out.AspectRatio != j.AspectRatio || !sameUpload {
		return ErrIdempotencyMismatch
	}
	return nil
}
func (f *StudioFacade) CreateAsset(c context.Context, user string, in request_open_api.CreateAssetRequest) (*dto_open_api.AssetDTO, error) {
	uid, e := uuid.Parse(user)
	if e != nil {
		return nil, ErrUnauthorized
	}
	if strings.TrimSpace(in.Name) == "" || in.JobID != nil && in.UploadID != nil {
		return nil, ErrValidation
	}
	a := &models.LibraryAsset{UserID: uid, Name: strings.TrimSpace(in.Name), AssetType: in.AssetType, Description: in.Description, Prompt: in.Prompt, Scope: "personal", Version: 1}
	if in.JobID != nil {
		j, e := f.jobs.FindByID(c, *in.JobID, user)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if j.Status != models.JobStatusSucceeded || j.OutputMediaKey == nil || j.OutputMediaType == nil || *j.OutputMediaType != "image" || !durableKey(*j.OutputMediaKey) {
			return nil, ErrAssetSource
		}
		var sourceSnapshot struct {
			Category string `json:"category"`
		}
		if len(j.EffectiveInputs) > 0 && json.Unmarshal(j.EffectiveInputs, &sourceSnapshot) != nil {
			return nil, ErrAssetSource
		}
		if sourceSnapshot.Category == "product" && in.AssetType != "prop" {
			return nil, ErrValidation
		}
		a.JobID = &j.ID
		a.StorageKey = j.OutputMediaKey
		a.MediaType = j.OutputMediaType
		a.Prompt = j.Prompt
	}
	if in.UploadID != nil {
		u, e := f.uploads.FindByID(c, *in.UploadID, user)
		if e != nil {
			return nil, mapRepoErr(e)
		}
		if u.ExpiresAt.Before(time.Now()) || !strings.HasPrefix(u.MimeType, "image/") || !durableKey(u.StorageKey) {
			return nil, ErrAssetSource
		}
		a.UploadID = &u.ID
		a.StorageKey = &u.StorageKey
		kind := "image"
		a.MediaType = &kind
	}
	if e = f.repo.SaveAsset(c, a); e != nil {
		if errors.Is(e, repo.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, e
	}
	return f.assetView(c, a)
}
func durableKey(s string) bool {
	return s != "" && !strings.Contains(s, "://") && !strings.HasPrefix(s, "/") && !strings.Contains(s, "..")
}
func (f *StudioFacade) assetView(c context.Context, a *models.LibraryAsset) (*dto_open_api.AssetDTO, error) {
	d := &dto_open_api.AssetDTO{ID: a.ID.String(), Name: a.Name, AssetType: a.AssetType, Description: a.Description, Prompt: a.Prompt, Scope: a.Scope, Version: a.Version, Status: "draft", CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339)}
	if a.JobID != nil {
		v := a.JobID.String()
		d.JobID = &v
	}
	if a.UploadID != nil {
		v := a.UploadID.String()
		d.UploadID = &v
	}
	if a.StorageKey != nil {
		url, exp, e := f.storage.SignedURL(c, *a.StorageKey, time.Hour)
		if e != nil {
			return nil, e
		}
		d.MediaURL = &url
		s := exp.UTC().Format(time.RFC3339)
		d.MediaExpiresAt = &s
		d.Status = "ready"
	}
	return d, nil
}
func (f *StudioFacade) GetAsset(c context.Context, id, user string) (*dto_open_api.AssetDTO, error) {
	a, e := f.repo.Asset(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	return f.assetView(c, a)
}
func (f *StudioFacade) ListAssets(c context.Context, user, kind, search string, page, limit int) (*dto_open_api.AssetListDTO, error) {
	rows, n, e := f.repo.Assets(c, user, kind, search, page, limit)
	if e != nil {
		return nil, e
	}
	d := &dto_open_api.AssetListDTO{Items: make([]dto_open_api.AssetDTO, 0, len(rows)), Total: int(n), Page: page, Limit: limit}
	for i := range rows {
		a, e := f.assetView(c, &rows[i])
		if e != nil {
			return nil, e
		}
		d.Items = append(d.Items, *a)
	}
	return d, nil
}
func (f *StudioFacade) UpdateAsset(c context.Context, id, user string, in request_open_api.UpdateAssetRequest) (*dto_open_api.AssetDTO, error) {
	a, e := f.repo.Asset(c, id, user)
	if e != nil {
		return nil, mapRepoErr(e)
	}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return nil, ErrValidation
		}
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if e = f.repo.SaveAsset(c, a); e != nil {
		return nil, e
	}
	return f.assetView(c, a)
}
func (f *StudioFacade) DeleteAsset(c context.Context, id, user string) error {
	return mapRepoErr(f.repo.DeleteAsset(c, id, user))
}

func ImageSize(ratio string) string {
	return map[string]string{"1:1": "2048x2048", "3:4": "1728x2304", "4:3": "2304x1728", "16:9": "2560x1440", "9:16": "1440x2560"}[ratio]
}

// A durable pipeline pins the resolved provider model before a worker can restart.
func (f *StudioFacade) PipelineImageModel() string { return f.imageModel }
func (f *StudioFacade) CreatePipelineImage(c context.Context, user string, in request_open_api.CreateImageRequest, model string, renderingVersion string) (*dto_open_api.CreateJobResponseDTO, error) {
	frozen := *f
	frozen.imageModel = model
	return frozen.createImage(c, user, in, renderingVersion)
}
