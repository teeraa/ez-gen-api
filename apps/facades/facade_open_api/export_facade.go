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
	CreateExport(ctx context.Context, userID string, in CreateExportInput) (*dto_open_api.ExportDTO, error)
	GetExport(ctx context.Context, id, userID string) (*dto_open_api.ExportDTO, error)
	ListExports(ctx context.Context, userID string, page, limit int) (*dto_open_api.ExportListDTO, error)
	GetDownloadURL(ctx context.Context, id, userID string) (*dto_open_api.ExportDownloadDTO, error)
	// ProcessExport is called by the background worker to execute the merge.
	ProcessExport(ctx context.Context, export *models.StoryboardExport) error
}

// CreateExportInput is the validated payload for export creation.
type CreateExportInput struct {
	Clips         []ExportClip
	AudioUploadID *string
	AudioVolume   float64 // 0 → default 1.0
	TextLayers    []ExportTextLayer
}

// ExportClip is one scene clip with an optional trim window (seconds from clip start).
type ExportClip struct {
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
	exports   repository_open_api.ExportRepository
	jobs      repository_open_api.JobRepository
	uploads   repository_open_api.UploadRepository
	scenes    repository_open_api.SceneRepository
	storage   exportStorage
	processor videoprocessing.VideoProcessor
	http      *http.Client
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
	stor *storage.SupabaseClient,
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
	// Resolve to 2–20 clips, each a distinct scene.
	if len(in.Clips) < 2 || len(in.Clips) > 20 {
		return nil, ErrValidation
	}
	seen := make(map[string]struct{}, len(in.Clips))
	sceneIDs := make([]string, 0, len(in.Clips))
	for _, c := range in.Clips {
		if _, err := uuid.Parse(c.SceneID); err != nil {
			return nil, ErrValidation
		}
		if _, dup := seen[c.SceneID]; dup {
			return nil, ErrValidation
		}
		seen[c.SceneID] = struct{}{}
		if c.TrimEnd > 0 && c.TrimEnd <= c.TrimStart {
			return nil, ErrValidation
		}
		sceneIDs = append(sceneIDs, c.SceneID)
	}

	// Each scene must have a selected job with succeeded output.
	for _, sid := range sceneIDs {
		scene, err := f.scenes.FindByID(ctx, sid, userID)
		if err != nil {
			if errors.Is(err, repository_open_api.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("find scene %s: %w", sid, err)
		}
		if scene.SelectedJobID == nil {
			return nil, ErrSceneHasNoOutput
		}
		job, err := f.jobs.FindByID(ctx, scene.SelectedJobID.String(), userID)
		if err != nil || job.Status != models.JobStatusSucceeded || job.OutputMediaKey == nil {
			return nil, ErrSceneHasNoOutput
		}
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
		if !strings.HasPrefix(upload.MimeType, "audio/") {
			return nil, ErrInvalidFileType
		}
		audioStorageKey = &upload.StorageKey
	}

	vol := in.AudioVolume
	if vol <= 0 {
		vol = 1.0
	}

	sceneIDsJSON, err := json.Marshal(sceneIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal scene ids: %w", err)
	}

	storedClips := make([]models.StoredClip, len(in.Clips))
	for i, c := range in.Clips {
		storedClips[i] = models.StoredClip{
			SceneID:   c.SceneID,
			TrimStart: c.TrimStart,
			TrimEnd:   c.TrimEnd,
		}
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
		log.Printf("[export] export=%s failed: %v", export.ID, err)
		_ = f.exports.UpdateStatus(ctx, export.ID.String(), models.ExportStatusFailed, map[string]any{
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
		scene, err := f.scenes.FindByID(ctx, c.SceneID, export.UserID)
		if err != nil || scene.SelectedJobID == nil {
			return fmt.Errorf("scene %s has no selected job", c.SceneID)
		}
		job, err := f.jobs.FindByID(ctx, scene.SelectedJobID.String(), export.UserID)
		if err != nil || job.OutputMediaKey == nil {
			return fmt.Errorf("scene %s job has no output", c.SceneID)
		}
		signedURL, _, err := f.storage.SignedURL(ctx, *job.OutputMediaKey, 30*time.Minute)
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

	videoBytes, err := f.processor.Merge(ctx, videoprocessing.MergeRequest{
		Clips:       procClips,
		AudioPath:   audioPath,
		AudioVolume: export.AudioVolume,
		TextLayers:  textLayers,
	})
	if err != nil {
		return fmt.Errorf("video merge: %w", err)
	}

	outputKey := storage.KeyForExport(export.UserID, export.ID.String(), "mp4")
	if err := f.storage.Upload(ctx, outputKey, videoBytes, "video/mp4"); err != nil {
		return fmt.Errorf("upload export: %w", err)
	}

	return f.exports.UpdateStatus(ctx, export.ID.String(), models.ExportStatusSucceeded, map[string]any{
		"output_storage_key": outputKey,
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
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func mapExportDTO(e *models.StoryboardExport) *dto_open_api.ExportDTO {
	dto := &dto_open_api.ExportDTO{
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
