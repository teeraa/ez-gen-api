package crons

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"time"
)

type VideoEditorWorker struct {
	facade   *facade_open_api.VideoEditorFacade
	interval time.Duration
}

func NewVideoEditorWorker(f *facade_open_api.VideoEditorFacade, interval time.Duration) *VideoEditorWorker {
	return &VideoEditorWorker{facade: f, interval: interval}
}
func (w *VideoEditorWorker) Start(ctx context.Context) {
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
