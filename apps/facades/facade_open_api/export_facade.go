package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
)

// ExportFacade handles storyboard export creation, status, and processing.
type ExportFacade interface {
	RetryExport(context.Context, string, string) (*dto_open_api.ExportDTO, error)
	CreateExport(ctx context.Context, userID string, in CreateExportInput) (*dto_open_api.ExportDTO, error)
	GetExport(ctx context.Context, id, userID string) (*dto_open_api.ExportDTO, error)
	ListExports(ctx context.Context, userID string, page, limit int) (*dto_open_api.ExportListDTO, error)
	GetDownloadURL(ctx context.Context, id, userID string) (*dto_open_api.ExportDownloadDTO, error)
	// ProcessExport is called by the background worker to execute the merge.
	ProcessExport(ctx context.Context, export *models.StoryboardExport) error
}

// CreateExportInput is the validated payload for export creation.
type CreateExportInput struct {
	ProjectID      *string
	IdempotencyKey *string
	Output         *models.ExportOutput
	Clips          []ExportClip
	AudioUploadID  *string
	AudioVolume    float64 // 0 → default 1.0
	TextLayers     []ExportTextLayer
}

// ExportClip is one scene clip with an optional trim window (seconds from clip start).
type ExportClip struct {
	JobID     *string
	SceneID   string
	TrimStart float64
	TrimEnd   float64
}

// ExportTextLayer is a validated text overlay attached to one scene.
type ExportTextLayer struct {
	SceneID  string
	Text     string
	Position string
	FontSize int
	Color    string

	HasXY bool
	X, Y  float64
	Align string

	StartTime float64
	EndTime   float64
}

type exportFacade struct {
	exports    repository_open_api.ExportRepository
	jobs       repository_open_api.JobRepository
	uploads    repository_open_api.UploadRepository
	scenes     repository_open_api.SceneRepository
	storage    exportStorage
	processor  videoprocessing.VideoProcessor
	http       *http.Client
	production *repository_open_api.ProductionRepository
}

type exportStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
	SignedDownloadURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, time.Time, error)
}

func NewExportFacade(
	exports repository_open_api.ExportRepository,
	jobs repository_open_api.JobRepository,
	uploads repository_open_api.UploadRepository,
	scenes repository_open_api.SceneRepository,
	stor exportStorage,
	processor videoprocessing.VideoProcessor,
) ExportFacade {
	return &exportFacade{
		exports:   exports,
		jobs:      jobs,
		uploads:   uploads,
		scenes:    scenes,
		storage:   stor,
		processor: processor,
		http:      &http.Client{Timeout: 10 * time.Minute},
	}
}

func (f *exportFacade) CreateExport(ctx context.Context, userID string, in CreateExportInput) (*dto_open_api.ExportDTO, error) {
	hash, err := exportRequestHash(in)
	if err != nil {
		return nil, ErrValidation
	}
	if in.IdempotencyKey != nil {
		existing, e := f.exports.FindByIdempotencyKey(ctx, *in.IdempotencyKey, userID)
		if e != nil && !errors.Is(e, repository_open_api.ErrNotFound) {
			return nil, e
		}
		if existing != nil {
			if existing.RequestHash == nil || *existing.RequestHash != hash {
				return nil, ErrConflict
			}
			return mapExportDTO(existing), nil
		}
	}
	storedClips, output, err := f.snapshotClips(ctx, userID, in)
	if err != nil {
		return nil, err
	}
	sceneIDs := make([]string, len(storedClips))
	for i, c := range storedClips {
		sceneIDs[i] = c.SceneID
	}
	// Validate audio upload if provided.
	var audioStorageKey *string
	if in.AudioUploadID != nil {
		upload, err := f.uploads.FindByID(ctx, *in.AudioUploadID, userID)
		if err != nil {
			if errors.Is(err, repository_open_api.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("find audio upload: %w", err)
		}
		if time.Now().After(upload.ExpiresAt) {
			return nil, ErrImageExpired
		}
		if !strings.HasPrefix(upload.MimeType, "audio/") {
			return nil, ErrInvalidFileType
		}
		audioStorageKey = &upload.StorageKey
	}

	vol := in.AudioVolume
	if vol < 0 || vol > 2 {
		return nil, ErrValidation
	}

	sceneIDsJSON, err := json.Marshal(sceneIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal scene ids: %w", err)
	}

	clipsJSON, err := json.Marshal(storedClips)
	if err != nil {
		return nil, fmt.Errorf("marshal clips: %w", err)
	}

	export := &models.StoryboardExport{
		UserID:          userID,
		SceneIDs:        sceneIDsJSON,
		Clips:           clipsJSON,
		AudioStorageKey: audioStorageKey,
		AudioVolume:     vol,
		Status:          models.ExportStatusQueued,
		IdempotencyKey:  in.IdempotencyKey, RequestHash: &hash,
	}

	if in.ProjectID != nil {
		pid, _ := uuid.Parse(*in.ProjectID)
		export.ProjectID = &pid
	}
	export.OutputConfig, _ = json.Marshal(output)
	if e := validateExportText(in.TextLayers, sceneIDs); e != nil {
		return nil, e
	}
	if len(in.TextLayers) > 0 {
		stored := make([]models.StoredTextLayer, len(in.TextLayers))
		for i, tl := range in.TextLayers {
			stored[i] = models.StoredTextLayer{
				SceneID:   tl.SceneID,
				Text:      tl.Text,
				Position:  tl.Position,
				FontSize:  tl.FontSize,
				Color:     tl.Color,
				HasXY:     tl.HasXY,
				X:         tl.X,
				Y:         tl.Y,
				Align:     tl.Align,
				StartTime: tl.StartTime,
				EndTime:   tl.EndTime,
			}
		}
		b, err := json.Marshal(stored)
		if err != nil {
			return nil, fmt.Errorf("marshal text layers: %w", err)
		}
		export.TextLayers = b
	}

	if err := f.exports.Create(ctx, export); err != nil {
		if errors.Is(err, repository_open_api.ErrConflict) && in.IdempotencyKey != nil {
			existing, e := f.exports.FindByIdempotencyKey(ctx, *in.IdempotencyKey, userID)
			if e == nil && existing.RequestHash != nil && *existing.RequestHash == hash {
				return mapExportDTO(existing), nil
			}
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("create export: %w", err)
	}
	return mapExportDTO(export), nil
}

func (f *exportFacade) GetExport(ctx context.Context, id, userID string) (*dto_open_api.ExportDTO, error) {
	export, err := f.exports.FindByID(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get export: %w", err)
	}
	return mapExportDTO(export), nil
}

func (f *exportFacade) ListExports(ctx context.Context, userID string, page, limit int) (*dto_open_api.ExportListDTO, error) {
	exports, total, err := f.exports.List(ctx, userID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("list exports: %w", err)
	}
	items := make([]dto_open_api.ExportDTO, len(exports))
	for i, e := range exports {
		eCopy := e
		items[i] = *mapExportDTO(&eCopy)
	}
	return &dto_open_api.ExportListDTO{
		Items: items,
		Total: int(total),
		Page:  page,
		Limit: limit,
	}, nil
}

func (f *exportFacade) GetDownloadURL(ctx context.Context, id, userID string) (*dto_open_api.ExportDownloadDTO, error) {
	export, err := f.exports.FindByID(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repository_open_api.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get export: %w", err)
	}
	if export.Status != models.ExportStatusSucceeded || export.OutputStorageKey == nil {
		return nil, ErrExportNotReady
	}
	filename := fmt.Sprintf("export-%s.mp4", export.ID)
	signedURL, expiresAt, err := f.storage.SignedDownloadURL(ctx, *export.OutputStorageKey, time.Hour, filename)
	if err != nil {
		return nil, fmt.Errorf("sign download url: %w", err)
	}
	return &dto_open_api.ExportDownloadDTO{
		URL:       signedURL,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// ProcessExport is the worker entry point: download clips, run FFmpeg, upload result.
func (f *exportFacade) ProcessExport(ctx context.Context, export *models.StoryboardExport) error {
	now := time.Now()
	if err := f.exports.UpdateStatus(ctx, export.ID.String(), models.ExportStatusProcessing, map[string]any{
		"started_at": now,
	}); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	if err := f.doProcess(ctx, export); err != nil {
		msg := sanitizeExportError(err)
		log.Printf("[export] export=%s processing failed", export.ID)
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = f.exports.Finish(cleanup, export.ID.String(), export.LastPickedAt, models.ExportStatusFailed, map[string]any{
			"failure_message": msg,
			"completed_at":    time.Now(),
		})
		return err
	}
	return nil
}

func (f *exportFacade) doProcess(ctx context.Context, export *models.StoryboardExport) error {
	tmpDir, err := os.MkdirTemp("", "violet-dl-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	clips, err := exportClips(export)
	if err != nil {
		return err
	}

	// Download each scene's selected job clip.
	sceneIDs := make([]string, len(clips))
	procClips := make([]videoprocessing.Clip, len(clips))
	for i, c := range clips {
		sceneIDs[i] = c.SceneID
		key := c.StorageKey
		if key == "" { // Legacy exports resolve selection only until rewritten by a new request.
			scene, err := f.scenes.FindByID(ctx, c.SceneID, export.UserID)
			if err != nil || scene.SelectedJobID == nil {
				return ErrSceneHasNoOutput
			}
			job, err := f.jobs.FindByID(ctx, scene.SelectedJobID.String(), export.UserID)
			if err != nil || !isSceneVideo(job, scene.ID) {
				return ErrSceneHasNoOutput
			}
			key = *job.OutputMediaKey
		}
		signedURL, _, err := f.storage.SignedURL(ctx, key, 30*time.Minute)
		if err != nil {
			return fmt.Errorf("sign clip url scene=%s: %w", c.SceneID, err)
		}
		clipPath := filepath.Join(tmpDir, fmt.Sprintf("clip_%02d.mp4", i))
		if err := f.downloadToFile(ctx, signedURL, clipPath); err != nil {
			return fmt.Errorf("download clip scene=%s: %w", c.SceneID, err)
		}
		procClips[i] = videoprocessing.Clip{
			Path:      clipPath,
			TrimStart: c.TrimStart,
			TrimEnd:   c.TrimEnd,
		}
	}

	// Download audio if provided.
	var audioPath *string
	if export.AudioStorageKey != nil {
		signedURL, _, err := f.storage.SignedURL(ctx, *export.AudioStorageKey, 30*time.Minute)
		if err != nil {
			return fmt.Errorf("sign audio url: %w", err)
		}
		p := filepath.Join(tmpDir, "audio.bin")
		if err := f.downloadToFile(ctx, signedURL, p); err != nil {
			return fmt.Errorf("download audio: %w", err)
		}
		audioPath = &p
	}

	// Resolve text layers: map sceneID → clip index.
	textLayers, err := resolveTextLayers(export.TextLayers, sceneIDs)
	if err != nil {
		return fmt.Errorf("resolve text layers: %w", err)
	}

	output := defaultExportOutput()
	_ = json.Unmarshal(export.OutputConfig, &output)
	videoBytes, err := f.processor.Merge(ctx, videoprocessing.MergeRequest{
		Clips:            procClips,
		OutputResolution: output.Resolution, OutputAspectRatio: output.AspectRatio,
		AudioPath:   audioPath,
		AudioVolume: export.AudioVolume,
		TextLayers:  textLayers,
	})
	if err != nil {
		return fmt.Errorf("video merge: %w", err)
	}

	validated, err := os.CreateTemp(tmpDir, "validated-*.mp4")
	if err != nil {
		return err
	}
	if _, err = validated.Write(videoBytes); err != nil {
		validated.Close()
		return err
	}
	validated.Close()
	durationMs, err := videoprocessing.ProbeVideo(ctx, validated.Name())
	if err != nil {
		return fmt.Errorf("invalid export output: %w", err)
	}
	outputKey := storage.KeyForExport(export.UserID, export.ID.String(), "mp4")
	if err := f.storage.Upload(ctx, outputKey, videoBytes, "video/mp4"); err != nil {
		return fmt.Errorf("upload export: %w", err)
	}

	return f.exports.Finish(ctx, export.ID.String(), export.LastPickedAt, models.ExportStatusSucceeded, map[string]any{
		"output_storage_key": outputKey,
		"output_duration_ms": durationMs,
		"completed_at":       time.Now(),
	})
}

// exportClips returns the ordered clips for an export, falling back to the legacy
// scene_ids column (untrimmed) for rows written before the clips column existed.
func exportClips(export *models.StoryboardExport) ([]models.StoredClip, error) {
	if len(export.Clips) > 0 {
		var clips []models.StoredClip
		if err := json.Unmarshal(export.Clips, &clips); err != nil {
			return nil, fmt.Errorf("parse clips: %w", err)
		}
		if len(clips) > 0 {
			return clips, nil
		}
	}
	var sceneIDs []string
	if err := json.Unmarshal(export.SceneIDs, &sceneIDs); err != nil {
		return nil, fmt.Errorf("parse scene ids: %w", err)
	}
	clips := make([]models.StoredClip, len(sceneIDs))
	for i, sid := range sceneIDs {
		clips[i] = models.StoredClip{SceneID: sid}
	}
	return clips, nil
}

// resolveTextLayers maps StoredTextLayer.SceneID to a 0-based clip index.
func resolveTextLayers(raw []byte, sceneIDs []string) ([]videoprocessing.TextLayer, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var stored []models.StoredTextLayer
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("unmarshal text layers: %w", err)
	}
	idx := make(map[string]int, len(sceneIDs))
	for i, sid := range sceneIDs {
		idx[sid] = i
	}
	layers := make([]videoprocessing.TextLayer, 0, len(stored))
	for _, s := range stored {
		i, ok := idx[s.SceneID]
		if !ok {
			continue
		}
		layers = append(layers, videoprocessing.TextLayer{
			ClipIdx:   i,
			Text:      s.Text,
			Position:  s.Position,
			FontSize:  s.FontSize,
			Color:     s.Color,
			HasXY:     s.HasXY,
			X:         s.X,
			Y:         s.Y,
			Align:     s.Align,
			StartTime: s.StartTime,
			EndTime:   s.EndTime,
		})
	}
	return layers, nil
}

func (f *exportFacade) downloadToFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func sanitizeExportError(err error) string {
	return "ไม่สามารถส่งออกวิดีโอได้ โปรดตรวจคลิปต้นฉบับและลองอีกครั้ง"
}

func mapExportDTO(e *models.StoryboardExport) *dto_open_api.ExportDTO {
	output := defaultExportOutput()
	_ = json.Unmarshal(e.OutputConfig, &output)
	var pid *string
	if e.ProjectID != nil {
		x := e.ProjectID.String()
		pid = &x
	}
	dto := &dto_open_api.ExportDTO{
		ProjectID: pid, Output: output, OutputDurationMs: e.OutputDurationMs,
		ExportID:  e.ID.String(),
		Status:    string(e.Status),
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.CompletedAt != nil {
		s := e.CompletedAt.UTC().Format(time.RFC3339)
		dto.CompletedAt = &s
	}
	if e.FailureMessage != nil {
		dto.Error = e.FailureMessage
	}
	return dto
}
