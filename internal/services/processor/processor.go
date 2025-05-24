package processor

import (
	"context"
	"encoding/json"
	"github.com/cenkalti/backoff/v4"
	"github.com/weeb-vip/image-sync/internal/logger"
	"go.uber.org/zap"
)

type ProcessorFunc[T any] func(ctx context.Context, data T) error

type ProcessorImpl[T any] interface {
	Parse(ctx context.Context, payload string) (*T, error)
	Process(ctx context.Context, payload string, fn ProcessorFunc[T]) error
}

type Processor[T any] struct {
}

func NewProcessor[T any]() *Processor[T] {
	return &Processor[T]{}
}

func (p *Processor[T]) Parse(ctx context.Context, payload string) (*T, error) {
	// log raw payload as string
	log := logger.FromCtx(ctx)
	log.Info("Raw payload: ", zap.String("payload", payload))
	// parse from json
	var data T
	err := json.Unmarshal([]byte(payload), &data)
	if err != nil {
		return nil, err
	}
	return &data, nil

}

func (p *Processor[T]) Process(ctx context.Context, payload string, fn ProcessorFunc[T]) error {
	data, err := p.Parse(ctx, payload)
	if err != nil {
		return err
	}

	// do something with data

	operation := func() error {
		return fn(ctx, *data)
	}

	err = backoff.Retry(operation, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10))
	if err != nil {
		// Handle error.
		return err
	}
	return nil
}
