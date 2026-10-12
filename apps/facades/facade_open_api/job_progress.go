package facade_open_api

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
)

const (
	StepQueued     = "queued"
	StepGenerating = "generating"
	StepSaving     = "saving"
	StepDone       = "done"
	StepFailed     = "failed"
	StepCancelled  = "cancelled"
)

// BytePlus reports only task status, never a percentage, so progress is estimated
// from elapsed time against how long similar jobs took recently.
const (
	progressQueued        = 2
	progressGenStart      = 5
	progressGenOnSchedule = 85
	progressGenCeiling    = 90
	progressSaving        = 95
	progressDone          = 100

	estimateSampleSize = 20
	estimateMinSamples = 3
	estimateCacheTTL   = 5 * time.Minute
)

type estimateKey struct {
	modelID  string
	duration int
	quality  string
}

type cachedEstimate struct {
	seconds   float64
	expiresAt time.Time
}

// generationEstimator caches averages because the frontend polls every few seconds
// and the average barely moves between polls.
type generationEstimator struct {
	jobs  repository_open_api.JobRepository
	mu    sync.Mutex
	cache map[estimateKey]cachedEstimate
}

func newGenerationEstimator(jobs repository_open_api.JobRepository) *generationEstimator {
	return &generationEstimator{jobs: jobs, cache: make(map[estimateKey]cachedEstimate)}
}

func (e *generationEstimator) expectedSeconds(ctx context.Context, job *models.VideoGenerationJob) float64 {
	key := estimateKey{modelID: job.ModelID, duration: job.DurationSeconds, quality: job.Quality}
	now := time.Now()

	e.mu.Lock()
	if c, ok := e.cache[key]; ok && now.Before(c.expiresAt) {
		e.mu.Unlock()
		return c.seconds
	}
	e.mu.Unlock()

	seconds := e.lookup(ctx, key)

	e.mu.Lock()
	e.cache[key] = cachedEstimate{seconds: seconds, expiresAt: now.Add(estimateCacheTTL)}
	e.mu.Unlock()
	return seconds
}

// lookup prefers the same model, then any model at the same duration and quality,
// then a rough default for a fresh database.
func (e *generationEstimator) lookup(ctx context.Context, key estimateKey) float64 {
	for _, modelID := range []string{key.modelID, ""} {
		avg, n, err := e.jobs.AverageGenerationSeconds(ctx, modelID, key.duration, key.quality, estimateSampleSize)
		if err != nil {
			log.Printf("[progress] average generation time: %v", err)
			break
		}
		if n >= estimateMinSamples && avg > 0 {
			return avg
		}
	}
	return defaultGenerationSeconds(key.duration)
}

func defaultGenerationSeconds(durationSeconds int) float64 {
	return 30 + 6*float64(durationSeconds)
}

// applyProgress fills progress, currentStep and estimatedSecondsRemaining. Progress runs
// linearly to 85% at the expected finish time, then creeps toward 90% so a slow job never
// looks finished before the provider says so.
func (f *jobFacade) applyProgress(ctx context.Context, job *models.VideoGenerationJob, dto *dto_open_api.JobDTO) {
	setStep := func(step string) { dto.CurrentStep = &step }
	dto.EstimatedSecondsRemaining = nil

	switch job.Status {
	case models.JobStatusSucceeded:
		dto.Progress = progressDone
		setStep(StepDone)
		return
	case models.JobStatusFailed:
		setStep(StepFailed)
		return
	case models.JobStatusCancelled:
		setStep(StepCancelled)
		return
	}

	expected := f.estimator.expectedSeconds(ctx, job)

	if job.Status == models.JobStatusQueued {
		dto.Progress = progressQueued
		setStep(StepQueued)
		remaining := int(math.Ceil(expected))
		dto.EstimatedSecondsRemaining = &remaining
		return
	}

	if job.CurrentStep != nil && *job.CurrentStep == StepSaving {
		dto.Progress = progressSaving
		setStep(StepSaving)
		return
	}

	started := job.CreatedAt
	if job.SubmittedAt != nil {
		started = *job.SubmittedAt
	}
	elapsed := time.Since(started).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	dto.Progress = generationProgress(elapsed, expected)
	setStep(StepGenerating)
	remaining := int(math.Ceil(math.Max(expected-elapsed, 0)))
	dto.EstimatedSecondsRemaining = &remaining
}

func generationProgress(elapsed, expected float64) int {
	if expected <= 0 {
		expected = 1
	}
	var p float64
	if elapsed <= expected {
		p = progressGenStart + (progressGenOnSchedule-progressGenStart)*(elapsed/expected)
	} else {
		overrun := (elapsed - expected) / expected
		p = progressGenOnSchedule + (progressGenCeiling-progressGenOnSchedule)*(1-math.Exp(-overrun))
	}
	return int(math.Floor(p))
}
