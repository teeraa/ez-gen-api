package crons

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
)

// DirectorWorker runs several claim loops so storyboard images of one project (and jobs of
// different projects) are generated at the same time. Claim uses SKIP LOCKED, so loops never
// take the same job.
type DirectorWorker struct {
	facade *facade_open_api.DirectorFacade
	n      int
}

func NewDirectorWorker(f *facade_open_api.DirectorFacade, n int) *DirectorWorker {
	if n < 1 {
		n = 1
	}
	return &DirectorWorker{f, n}
}

func (w *DirectorWorker) Start(c context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.loop(c, time.Duration(i)*2*time.Second/time.Duration(w.n))
		}(i)
	}
	wg.Wait()
}

// loop claims again straight after finishing a job; when nothing could be claimed it waits a
// jittered interval so the loops spread out instead of racing for the same project lock.
func (w *DirectorWorker) loop(c context.Context, offset time.Duration) {
	wait := offset
	for {
		select {
		case <-c.Done():
			return
		case <-time.After(wait):
		}
		if e := w.facade.Tick(c); e == nil {
			wait = 0
			continue
		}
		wait = 1500*time.Millisecond + time.Duration(rand.Int63n(int64(time.Second)))
	}
}
