package crons

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"time"
)

type StoryFlowWorker struct {
	facade *facade_open_api.StoryFlowFacade
}

func NewStoryFlowWorker(f *facade_open_api.StoryFlowFacade) *StoryFlowWorker {
	return &StoryFlowWorker{f}
}
func (w *StoryFlowWorker) Start(c context.Context) {
	ticker := time.NewTicker(3 * time.Second)
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
