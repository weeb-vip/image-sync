package workerpool

import (
	"context"
	"sync"
	"github.com/ThatCatDev/ep/v2/drivers"
	"github.com/ThatCatDev/ep/v2/event"
)

type Config struct {
	Workers    int
	BufferSize int
}

type WorkerPoolMiddleware[MessageType, PayloadType any] struct {
	driver     drivers.Driver[MessageType]
	config     Config
	workerPool *workerPool[MessageType, PayloadType]
	processor  func(ctx context.Context, data event.Event[MessageType, PayloadType]) (event.Event[MessageType, PayloadType], error)
}

type workerPool[MessageType, PayloadType any] struct {
	workers   int
	jobQueue  chan job[MessageType, PayloadType]
	quit      chan bool
	wg        sync.WaitGroup
	processor func(ctx context.Context, data event.Event[MessageType, PayloadType]) (event.Event[MessageType, PayloadType], error)
}

type job[MessageType, PayloadType any] struct {
	ctx  context.Context
	data event.Event[MessageType, PayloadType]
	done chan result[MessageType, PayloadType]
}

type result[MessageType, PayloadType any] struct {
	data event.Event[MessageType, PayloadType]
	err  error
}

func NewWorkerPoolMiddleware[MessageType, PayloadType any](
	driver drivers.Driver[MessageType],
	config Config,
) *WorkerPoolMiddleware[MessageType, PayloadType] {
	return &WorkerPoolMiddleware[MessageType, PayloadType]{
		driver: driver,
		config: config,
	}
}

func (w *WorkerPoolMiddleware[MessageType, PayloadType]) SetNextProcessor(
	processor func(ctx context.Context, data event.Event[MessageType, PayloadType]) (event.Event[MessageType, PayloadType], error),
) {
	w.processor = processor
	w.workerPool = &workerPool[MessageType, PayloadType]{
		workers:   w.config.Workers,
		jobQueue:  make(chan job[MessageType, PayloadType], w.config.BufferSize),
		quit:      make(chan bool),
		processor: processor,
	}
}

func (w *WorkerPoolMiddleware[MessageType, PayloadType]) Start(ctx context.Context) {
	if w.workerPool != nil {
		w.workerPool.start(ctx)
	}
}

func (w *WorkerPoolMiddleware[MessageType, PayloadType]) Stop() {
	if w.workerPool != nil {
		w.workerPool.stop()
	}
}

func (w *WorkerPoolMiddleware[MessageType, PayloadType]) Process(
	ctx context.Context,
	data event.Event[MessageType, PayloadType],
) (event.Event[MessageType, PayloadType], error) {
	if w.workerPool == nil {
		// Fallback to direct processing if worker pool not initialized
		return w.processor(ctx, data)
	}

	done := make(chan result[MessageType, PayloadType], 1)
	j := job[MessageType, PayloadType]{
		ctx:  ctx,
		data: data,
		done: done,
	}

	select {
	case w.workerPool.jobQueue <- j:
		// Job submitted successfully
	case <-ctx.Done():
		return data, ctx.Err()
	}

	select {
	case res := <-done:
		return res.data, res.err
	case <-ctx.Done():
		return data, ctx.Err()
	}
}

func (p *workerPool[MessageType, PayloadType]) start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

func (p *workerPool[MessageType, PayloadType]) stop() {
	close(p.quit)
	p.wg.Wait()
	close(p.jobQueue)
}

func (p *workerPool[MessageType, PayloadType]) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	for {
		select {
		case j := <-p.jobQueue:
			data, err := p.processor(j.ctx, j.data)
			j.done <- result[MessageType, PayloadType]{data: data, err: err}
			close(j.done)
		case <-p.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}