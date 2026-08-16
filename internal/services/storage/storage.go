package storage

import "context"

// Entry is one object seen while listing. Path is bucket-prefix-relative and
// leading-slashed, i.e. exactly what Put/Get/Copy take.
type Entry struct {
	Path string
	Err  error
}

type Storage interface {
	Put(ctx context.Context, data []byte, path string) error
	Get(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
	// List walks objects under path. Non-recursive listing returns the
	// immediate children only, and skips the pseudo-directories, so the anime
	// posters at the bucket root can be walked without pulling in
	// characters/, staff/ and banners/.
	List(ctx context.Context, path string, recursive bool) <-chan Entry
	// Copy duplicates an object server-side; the source is left in place.
	Copy(ctx context.Context, srcPath, dstPath string) error
	Exists(ctx context.Context, path string) (bool, error)
}
