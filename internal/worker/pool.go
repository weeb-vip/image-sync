package worker

import (
	"context"
	"sync"
)

type Job[T any] struct {
	Data T
	Done chan error
}

type Pool[T any] struct {
	workers    int
	jobQueue   chan Job[T]
	quit       chan bool
	wg         sync.WaitGroup
	processor  func(ctx context.Context, data T) error
}

func NewPool[T any](workers int, bufferSize int, processor func(ctx context.Context, data T) error) *Pool[T] {
	return &Pool[T] {
		workers:   workers,
		jobQueue:  make(chan Job[T], bufferSize),
		quit:      make(chan bool),
		processor: processor,
	}
}

func (p *Pool[T]) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

func (p *Pool[T]) Submit(data T) chan error {
	done := make(chan error, 1)
	job := Job[T]{
		Data: data,
		Done: done,
	}
	p.jobQueue <- job
	return done
}

func (p *Pool[T]) Stop() {
	close(p.quit)
	p.wg.Wait()
	close(p.jobQueue)
}

func (p *Pool[T]) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	for {
		select {
		case job := <-p.jobQueue:
			err := p.processor(ctx, job.Data)
			job.Done <- err
			close(job.Done)
		case <-p.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}