# Skill: video-generation-lifecycle

Implement the complete video generation lifecycle at the facade and cron layers:
job creation → provider submission → status polling → output persistence →
terminal state handling. Covers concurrent safety, idempotency, stale detection,
and cancellation.

## When to Invoke

- Implementing `apps/facades/facade_video_generation/`
- Implementing `apps/crons/video_generation_poller.go`
- Debugging a job stuck in `submitted` or `processing` state
- Adding cancellation support
- Adding retry logic after a transient provider failure

---

## Job State Machine (Authoritative)

```
           [POST /jobs]
                │
                ▼
           ┌─────────┐
           │ queued  │  persisted; provider not yet called
           └────┬────┘
                │ facade: CreateTask() → provider accepts
                ▼
           ┌──────────┐
           │submitted │  provider task ID obtained; poller begins
           └────┬─────┘
                │ poller: provider status == "running"
                ▼
           ┌────────────┐
           │ processing │  provider is generating
           └──────┬─────┘
        ┌─────────┼─────────┐
        │ success │ failure │ user DELETE /jobs/:id
        ▼         ▼         ▼
   ┌─────────┐ ┌──────┐ ┌──────────┐
   │succeeded│ │failed│ │cancelled │
   └─────────┘ └──────┘ └──────────┘
     Terminal   Terminal   Terminal
```

Application status → provider status correspondence: see
`docs/engineering/video-data-model-proposal.md` Section 4.

---

## Facade: Job Creation

`apps/facades/facade_video_generation/create_job.go`:

```go
func (f *facade) CreateJob(ctx context.Context, userID string, req request_video_generation.CreateJobRequest) (*dto_video_generation.CreateJobResponse, error) {
    // 1. Idempotency check
    if req.IdempotencyKey != nil {
        existing, err := f.repo.FindByIdempotencyKey(ctx, userID, *req.IdempotencyKey)
        if err == nil { // found
            return mapToCreateResponse(existing), nil
        }
        if !errors.Is(err, ErrNotFound) {
            return nil, fmt.Errorf("idempotency check: %w", err)
        }
    }

    // 2. Model capability validation
    errs := modelark.ValidateRequest(req.Model, req.Mode, req.AspectRatio, req.Duration, false)
    if len(errs) > 0 {
        return nil, &ValidationError{Fields: errs}
    }

    // 3. Validate reference image if image mode
    if req.Mode != "text" {
        if req.ReferenceImageID == nil {
            return nil, ErrImageRequired
        }
        upload, err := f.uploadRepo.FindByID(ctx, *req.ReferenceImageID, userID)
        if errors.Is(err, ErrNotFound) { return nil, ErrImageRequired }
        if err != nil { return nil, fmt.Errorf("validate upload: %w", err) }
        if time.Now().After(upload.ExpiresAt) { return nil, ErrImageExpired }
    }

    // 4. Estimate and reserve credits (non-blocking if credit system not yet implemented)
    estimated := estimateCredits(req.Model, req.Duration, req.Quality)

    // 5. Persist job in queued state
    job := &models.VideoGenerationJob{
        UserID:          uuid.MustParse(userID),
        Status:          "queued",
        Prompt:          req.Prompt,
        Mode:            req.Mode,
        ModelID:         req.Model,
        AspectRatio:     req.AspectRatio,
        DurationSeconds: req.Duration,
        Quality:         req.Quality,
        EnhancePrompt:   req.EnhancePrompt,
        IdempotencyKey:  req.IdempotencyKey,
        EstimatedCredits: &estimated,
    }
    if err := f.repo.Create(ctx, job); err != nil {
        return nil, fmt.Errorf("persist job: %w", err)
    }

    // 6. Submit to provider asynchronously (do not block the HTTP response)
    go f.submitToProvider(context.Background(), job.ID)

    return mapToCreateResponse(job), nil
}
```

**Design note:** Provider submission is async — the HTTP response returns immediately
with `status: queued`. The poller handles subsequent state transitions.

---

## Facade: Provider Submission

`apps/facades/facade_video_generation/submit.go`:

```go
func (f *facade) submitToProvider(ctx context.Context, jobID uuid.UUID) {
    job, err := f.repo.FindByID(ctx, jobID.String(), "")
    if err != nil { f.logSubmitError(jobID, err); return }

    // Compile prompt via Creative DNA compiler
    compiledPrompt, contentItems, err := f.dnaCompiler.Compile(ctx, job)
    if err != nil { f.failJob(ctx, job, "PROMPT_COMPILE_ERROR", err); return }

    // Submit to provider
    taskID, err := f.modelarkClient.CreateTask(ctx, modelark.CreateTaskRequest{
        ProviderModelID: cap.ProviderModelID,
        Content:         contentItems,
        // ... other params from job
    })
    if err != nil {
        // Retry or fail depending on error class
        f.handleSubmitError(ctx, job, err)
        return
    }

    // Transition: queued → submitted
    if err := f.repo.TransitionStatus(ctx, job.ID.String(), "queued", "submitted",
        func(j *models.VideoGenerationJob) {
            j.ProviderTaskID = &taskID
            j.SubmittedAt = timePtr(time.Now())
        }); err != nil {
        f.logSubmitError(jobID, err)
    }
}
```

---

## Status Poller

`apps/crons/video_generation_poller.go`:

### Poll Cycle Rules

1. Query jobs where `status IN ('submitted', 'processing')` AND
   `last_polled_at < NOW() - ARK_POLL_INTERVAL_SECONDS`
2. For each job, call `GetTask(providerTaskID)`
3. Apply status transition if changed
4. Update `last_polled_at` regardless of status change
5. On `succeeded`: immediately trigger media download (see `reference-assets-and-storage`)
6. Back off to 3× interval after 60 polls with no change
7. Mark job `failed` with `failure_code: STALE_JOB` after 6 hours without terminal status

```go
// Conditional status transition — prevents race with user cancellation
func (r *jobRepository) TransitionStatus(ctx context.Context, id, fromStatus, toStatus string, mutate func(*models.VideoGenerationJob)) error {
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        var job models.VideoGenerationJob
        if err := tx.Set("gorm:query_option", "FOR UPDATE").
            Where("id = ? AND status = ?", id, fromStatus).
            First(&job).Error; err != nil {
            return fmt.Errorf("lock job %s: %w", id, err)
        }
        if mutate != nil { mutate(&job) }
        job.Status = toStatus
        job.UpdatedAt = time.Now()
        return tx.Save(&job).Error
    })
}
```

---

## Cancellation Flow

```
User DELETE /open/v1/jobs/:id
         │
         ▼
    Check status (with SELECT FOR UPDATE)
         │
    ┌────┴────────────────────────────────┐
    │ status == queued or submitted       │ status == processing
    ▼                                     ▼
Try provider cancel (DeleteTask)    Set app status = cancelled
    │                                  (provider task continues but
    ├─ success → status = cancelled     output is discarded)
    └─ 409 (already running) →
       set app status = cancelled
       (provider task continues to
        run but output is discarded)
```

Both paths result in application status `cancelled`.
Provider-level cancellation is a best-effort optimization (saves cost on queued tasks).
It is NOT required for the application to correctly cancel a job.

---

## Stale Job Detection

A job is stale when `last_polled_at < NOW() - 6h` AND `status IN ('submitted', 'processing')`.

Poller action on stale job:
1. Call `GetTask` one final time.
2. If provider has a terminal status: apply it.
3. If provider returns 404 (task expired): mark job `failed`, `failure_code: PROVIDER_TASK_EXPIRED`.
4. If provider is still non-terminal: mark job `failed`, `failure_code: STALE_JOB`.

---

## Acceptance Criteria

- [ ] Idempotent job creation: duplicate `idempotency_key` returns existing job, not 409
- [ ] `TransitionStatus` uses `SELECT FOR UPDATE` and conditional `WHERE status = fromStatus`
- [ ] User cancellation never returns 403 for another user's job (returns 404)
- [ ] Poller does not overwrite a `cancelled` status set by the user
- [ ] Stale jobs are failed after 6h with correct `failure_code`
- [ ] Media persistence triggered synchronously before marking `succeeded`
- [ ] `go test -race ./apps/facades/...` passes
- [ ] `go test -race ./apps/crons/...` passes (requires mock provider and mock repo)

---

## Related Skills

- `modelark-provider-integration` — `CreateTask`, `GetTask` called by this facade
- `creative-dna-prompt-compiler` — `Compile()` called in `submitToProvider`
- `reference-assets-and-storage` — media download triggered on `succeeded`
- `modelark-integration-testing` — tests the state machine transitions
