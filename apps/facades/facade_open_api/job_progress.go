package facade_open_api

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
)

const (
	StepQueued     = "queued"
	StepGenerating = "generating"
	StepSaving     = "saving"
	StepDone       = "done"
	StepFailed     = "failed"
	StepCancelled  = "cancelled"
)

// applyProgress reflects provider states; no percentage or ETA is reported without provider evidence.
func (f *jobFacade) applyProgress(ctx context.Context, job *models.VideoGenerationJob, dto *dto_open_api.JobDTO) {
	dto.Progress = 0
	dto.EstimatedSecondsRemaining = nil
	if job.Mode == "text_image" {
		return
	}
	step := StepGenerating
	switch job.Status {
	case models.JobStatusQueued:
		step = StepQueued
	case models.JobStatusSucceeded:
		dto.Progress = 100
		step = StepDone
	case models.JobStatusFailed:
		step = StepFailed
	case models.JobStatusCancelled:
		step = StepCancelled
	default:
		if job.CurrentStep != nil && *job.CurrentStep == StepSaving {
			step = StepSaving
		}
	}
	dto.CurrentStep = &step
}
