package crons

import (
	"context"
	"log"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
)

// JobPoller polls pending jobs and advances their status via the provider.
type JobPoller struct {
	jobs     repository_open_api.JobRepository
	facade   facade_open_api.JobFacade
	interval time.Duration
	batch    int
}

func NewJobPoller(
	jobs repository_open_api.JobRepository,
	facade facade_open_api.JobFacade,
	interval time.Duration,
	batch int,
) *JobPoller {
	return &JobPoller{
		jobs:     jobs,
		facade:   facade,
		interval: interval,
		batch:    batch,
	}
}

// Start runs the poller until ctx is cancelled.
func (p *JobPoller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *JobPoller) poll(ctx context.Context) {
	jobs, err := p.jobs.ListPendingPoll(ctx, p.batch)
	if err != nil {
		log.Printf("job poller: list pending: %v", err)
		return
	}
	for _, job := range jobs {
		j := job
		if err := p.facade.PollAndUpdate(ctx, &j); err != nil {
			log.Printf("job poller: poll job %s: %v", j.ID, err)
		}
	}
}
