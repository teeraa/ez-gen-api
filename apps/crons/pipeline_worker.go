package crons

import (
	"context"
	"log"
	"time"
)

type pipelineRunner interface{ RunOnce(context.Context) error }
type PipelineWorker struct{ runner pipelineRunner }

func NewPipelineWorker(r pipelineRunner) *PipelineWorker { return &PipelineWorker{runner: r} }
func (w *PipelineWorker) Start(c context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.Done():
			return
		case <-t.C:
			if e := w.runner.RunOnce(c); e != nil {
				log.Print("studio pipeline operation could not be completed")
			}
		}
	}
}
