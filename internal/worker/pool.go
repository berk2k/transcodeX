package worker

import (
	"context"
	"log/slog"
	"sync"
)

type Pool struct {
	workerCount int
	jobs        chan Job
	wg          sync.WaitGroup
	logger      *slog.Logger
	processor   *Processor
}

type Job struct {
	MessageID     string
	ReceiptHandle string
	JobID         string
	S3Key         string
	Bucket        string
}

func NewPool(workerCount int, processor *Processor, logger *slog.Logger) *Pool {
	return &Pool{
		workerCount: workerCount,
		jobs:        make(chan Job, workerCount),
		logger:      logger,
		processor:   processor,
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.logger.Info("worker started", "workerID", id)
			for {
				select {
				case job, ok := <-p.jobs:
					if !ok {
						p.logger.Info("worker stopping", "workerID", id)
						return
					}
					p.processor.Process(ctx, job)
				case <-ctx.Done():
					p.logger.Info("worker stopping", "workerID", id)
					return
				}
			}
		}(i)
	}
}

func (p *Pool) Submit(job Job) {
	p.jobs <- job
}

func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
}
