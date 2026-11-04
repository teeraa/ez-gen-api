package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"math"
	"regexp"
	"strings"
	"time"
)

func ConfigureExportProduction(e ExportFacade, r *repo.ProductionRepository) {
	if f, ok := e.(*exportFacade); ok {
		f.production = r
	}
}
func defaultExportOutput() models.ExportOutput {
	return models.ExportOutput{Format: "mp4", Resolution: "720p", AspectRatio: "16:9"}
}
func exportRequestHash(in CreateExportInput) (string, error) {
	in.IdempotencyKey = nil
	b, e := json.Marshal(in)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (f *exportFacade) snapshotClips(c context.Context, user string, in CreateExportInput) ([]models.StoredClip, models.ExportOutput, error) {
	out := defaultExportOutput()
	if in.Output != nil {
		out = *in.Output
	}
	if out.Format != "mp4" {
		return nil, out, ErrValidation
	}
	if _, _, e := videoprocessing.OutputDimensions(out.Resolution, out.AspectRatio); e != nil {
		return nil, out, ErrValidation
	}
	if in.ProjectID != nil {
		if _, e := uuid.Parse(*in.ProjectID); e != nil {
			return nil, out, ErrValidation
		}
		if f.production == nil {
			return nil, out, ErrValidation
		}
		if _, e := f.production.Find(c, *in.ProjectID, user); e != nil {
			return nil, out, mapRepoErr(e)
		}
	}
	if len(in.Clips) < 1 || len(in.Clips) > 20 {
		return nil, out, ErrValidation
	}
	seen := map[string]bool{}
	clips := []models.StoredClip{}
	for _, cl := range in.Clips {
		if _, e := uuid.Parse(cl.SceneID); e != nil {
			return nil, out, ErrValidation
		}
		if seen[cl.SceneID] {
			return nil, out, ErrValidation
		}
		seen[cl.SceneID] = true
		s, e := f.scenes.FindByID(c, cl.SceneID, user)
		if e != nil {
			return nil, out, mapRepoErr(e)
		}
		if in.ProjectID != nil && (s.ProjectID == nil || s.ProjectID.String() != *in.ProjectID) {
			return nil, out, ErrNotFound
		}
		if f.production != nil {
			if e := f.production.ProjectForScene(c, s, user); e != nil {
				return nil, out, mapRepoErr(e)
			}
		}
		jid := s.SelectedJobID
		if cl.JobID != nil {
			p, e := uuid.Parse(*cl.JobID)
			if e != nil {
				return nil, out, ErrValidation
			}
			jid = &p
		}
		if jid == nil {
			return nil, out, ErrSceneHasNoOutput
		}
		j, e := f.jobs.FindByID(c, jid.String(), user)
		if e != nil || !isSceneVideo(j, s.ID) {
			return nil, out, ErrSceneHasNoOutput
		}
		var duration int64
		if j.OutputDurationMs != nil {
			duration = int64(*j.OutputDurationMs)
		}
		if !validTrim(cl.TrimStart, cl.TrimEnd, duration) {
			return nil, out, ErrValidation
		}
		clips = append(clips, models.StoredClip{SceneID: cl.SceneID, JobID: j.ID.String(), StorageKey: *j.OutputMediaKey, DurationMs: duration, TrimStart: cl.TrimStart, TrimEnd: cl.TrimEnd})
	}
	return clips, out, nil
}

var exportColor = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

func validateExportText(ts []ExportTextLayer, sceneIDs []string) error {
	if len(ts) > 100 {
		return ErrValidation
	}
	set := map[string]bool{}
	for _, id := range sceneIDs {
		set[id] = true
	}
	for _, t := range ts {
		if !set[t.SceneID] || strings.TrimSpace(t.Text) == "" || len([]rune(t.Text)) > 200 || t.FontSize < 0 || t.FontSize > 200 || t.Color != "" && !exportColor.MatchString(t.Color) || t.HasXY && (math.IsNaN(t.X) || math.IsNaN(t.Y) || t.X < 0 || t.X > 1 || t.Y < 0 || t.Y > 1) || !validTrim(t.StartTime, t.EndTime, 0) {
			return ErrValidation
		}
		if t.Position != "" && t.Position != "top" && t.Position != "center" && t.Position != "bottom" {
			return ErrValidation
		}
		if t.Align != "" && t.Align != "left" && t.Align != "center" && t.Align != "right" {
			return ErrValidation
		}
	}
	return nil
}
func (f *exportFacade) RetryExport(c context.Context, id, user string) (*dto.ExportDTO, error) {
	e, err := f.exports.FindByID(c, id, user)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if e.Status != models.ExportStatusFailed {
		return nil, ErrConflict
	}
	if f.production != nil && e.ProjectID != nil {
		if _, err = f.production.Find(c, e.ProjectID.String(), user); err != nil {
			return nil, mapRepoErr(err)
		}
	}
	if err = f.exports.Retry(c, id, user); err != nil {
		return nil, mapRepoErr(err)
	}
	e.Status = models.ExportStatusQueued
	e.FailureMessage = nil
	e.CompletedAt = nil
	return mapExportDTO(e), nil
}
func (f *ProductionFacade) ConfigureProcessor(p videoprocessing.VideoProcessor) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s, ok := p.(interface{ SupportsTextOverlays(context.Context) bool }); ok {
		f.supportsText = s.SupportsTextOverlays(ctx)
	}
}
