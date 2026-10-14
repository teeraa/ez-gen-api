package crons

import (
	"context"
	"log"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
)

// ExportWorker picks up queued storyboard exports and processes them via FFmpeg.
type ExportWorker struct {
	exports  repository_open_api.ExportRepository
	facade   facade_open_api.ExportFacade
	interval time.Duration
	batch    int
	// processTimeout is the per-export context deadline.
	// A typical 5-scene merge with audio takes ~60–120s on a modest server.
	processTimeout time.Duration
}

func NewExportWorker(
	exports repository_open_api.ExportRepository,
	facade facade_open_api.ExportFacade,
	interval time.Duration,
	batch int,
) *ExportWorker {
	return &ExportWorker{
		exports:        exports,
		facade:         facade,
		interval:       interval,
		batch:          batch,
		processTimeout: 10 * time.Minute,
	}
}

// Start runs the worker until ctx is cancelled.
func (w *ExportWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *ExportWorker) process(ctx context.Context) {
	exports, err := w.exports.ListQueued(ctx, w.batch)
	if err != nil {
		log.Printf("[export worker] list queued: %v", err)
		return
	}
	for _, e := range exports {
		exp := e
		processCtx, cancel := context.WithTimeout(ctx, w.processTimeout)
		if err := w.facade.ProcessExport(processCtx, &exp); err != nil {
			log.Printf("[export worker] export=%s: %v", exp.ID, err)
		}
		cancel()
	}
}
