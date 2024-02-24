package storage

import "context"

type Storage interface {
	Put(ctx context.Context, data []byte, path string) error
	Get(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
}
