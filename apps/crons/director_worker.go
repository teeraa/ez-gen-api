package crons

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"time"
)

type DirectorWorker struct {
	facade *facade_open_api.DirectorFacade
}

func NewDirectorWorker(f *facade_open_api.DirectorFacade) *DirectorWorker { return &DirectorWorker{f} }
func (w *DirectorWorker) Start(c context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Done():
			return
		case <-ticker.C:
			_ = w.facade.Tick(c)
		}
	}
}
