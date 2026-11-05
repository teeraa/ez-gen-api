package modelark

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"log"
	"time"
)

type UsageEvent struct {
	AttemptID       uuid.UUID
	UserID          uuid.UUID
	JobID           uuid.UUID
	Provider        string
	Model           string
	Operation       string
	ProviderTaskID  string
	Status          string
	StartedAt       time.Time
	CompletedAt     *time.Time
	LatencyMS       *int64
	InputTokens     *int64
	OutputTokens    *int64
	TotalTokens     *int64
	GeneratedImages *int64
	DurationSeconds *int64
}

type UsageRecorder func(context.Context, UsageEvent) error

type usageOwner struct{ userID, jobID uuid.UUID }
type usageOwnerKey struct{}

func WithUsageOwner(ctx context.Context, userID, jobID uuid.UUID) context.Context {
	return context.WithValue(ctx, usageOwnerKey{}, usageOwner{userID, jobID})
}

// ErrNotSubmitted marks failures that happen before any provider request is sent, so callers
// can refund and allow a retry instead of treating the outcome as uncertain.
var ErrNotSubmitted = errors.New("provider request not submitted")

func beginUsage(ctx context.Context, recorder UsageRecorder, model, operation string) (UsageEvent, error) {
	event := UsageEvent{AttemptID: uuid.New(), Provider: "byteplus", Model: model, Operation: operation, Status: "started", StartedAt: time.Now().UTC()}
	if recorder == nil {
		return event, nil
	}
	owner, ok := ctx.Value(usageOwnerKey{}).(usageOwner)
	if !ok || owner.userID == uuid.Nil || owner.jobID == uuid.Nil {
		return event, fmt.Errorf("%w: missing AI usage owner", ErrNotSubmitted)
	}
	event.UserID, event.JobID = owner.userID, owner.jobID
	if err := recorder(ctx, event); err != nil {
		return event, fmt.Errorf("%w: cannot record AI usage attempt", ErrNotSubmitted)
	}
	return event, nil
}

func recordUsage(ctx context.Context, recorder UsageRecorder, event UsageEvent) error {
	if recorder == nil {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return recorder(persistCtx, event)
}

func finishUsage(ctx context.Context, recorder UsageRecorder, event UsageEvent) {
	elapsed := time.Since(event.StartedAt).Milliseconds()
	event.LatencyMS = &elapsed
	if event.Status != "submitted" {
		now := time.Now().UTC()
		event.CompletedAt = &now
	}
	// A persistence failure must not turn a successful paid submission into a retry.
	if err := recordUsage(ctx, recorder, event); err != nil {
		log.Printf("AI usage completion persistence failed attempt=%s job=%s", event.AttemptID, event.JobID)
	}
}

func nonnegativeToken(n int64) *int64 {
	if n < 0 {
		return nil
	}
	return &n
}
