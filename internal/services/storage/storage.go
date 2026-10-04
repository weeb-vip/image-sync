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
	// Stat reports an object's size. Used to tell an unchanged image from a
	// genuinely new one without downloading it.
	Stat(ctx context.Context, path string) (size int64, found bool, err error)
	// Head reports an object's size and user metadata (lower-cased keys).
	Head(ctx context.Context, path string) (info Info, found bool, err error)
	// PutObject stores data with a content type and user metadata. Put is
	// PutObject with an octet-stream type and no metadata.
	PutObject(ctx context.Context, data []byte, path, contentType string, meta map[string]string) error
}

// Info is what Head answers.
type Info struct {
	Size int64
	Meta map[string]string
}

// Metadata keys written on every stored image.
const (
	// MetaSourceLength is the byte length of the source the object was
	// fetched from. The processor compares it with the source's
	// Content-Length to skip a re-download: comparing the object's own size
	// broke the moment anything rewrote the object -- an upscaled poster is
	// bigger than its source, looked "changed", and was overwritten with the
	// 225px original on the next event.
	MetaSourceLength = "source-length"
	// MetaSourceURL is where the bytes came from.
	MetaSourceURL = "source-url"
)
