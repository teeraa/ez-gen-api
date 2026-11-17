package crons

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
)

// FilmWorker runs the สร้างหนัง jobs (film_jobs). It never touches director_jobs.
type FilmWorker struct {
	facade *facade_open_api.FilmFacade
	n      int
}

func NewFilmWorker(f *facade_open_api.FilmFacade, n int) *FilmWorker {
	if n < 1 {
		n = 1
	}
	return &FilmWorker{f, n}
}

func (w *FilmWorker) Start(c context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wait := time.Duration(i) * 2 * time.Second / time.Duration(w.n)
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
		}(i)
	}
	wg.Wait()
}
