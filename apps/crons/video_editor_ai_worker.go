package crons

import (
	"context"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"time"
)

type VideoEditorAIWorker struct {
	facade   *f.VideoEditorAIFacade
	interval time.Duration
}

func NewVideoEditorAIWorker(facade *f.VideoEditorAIFacade, interval time.Duration) *VideoEditorAIWorker {
	return &VideoEditorAIWorker{facade, interval}
}
func (w *VideoEditorAIWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.facade.ProcessNext(ctx)
		}
	}
}
