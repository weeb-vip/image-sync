package minio

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
)

type MinioStorageImpl struct {
	Client *minio.Client
	Bucket string
	Prefix string
}

func NewMinioStorage(cfg config.MinioConfig) storage.Storage {
	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		panic(err)
	}
	return &MinioStorageImpl{
		Client: minioClient,
		Bucket: cfg.Bucket,
		Prefix: cfg.Prefix,
	}
}

// objectKey prepends the configured prefix so objects can live in a
// folder of a shared bucket (e.g. r2 with one custom domain per bucket)
func (m *MinioStorageImpl) objectKey(path string) string {
	if m.Prefix == "" {
		return path
	}
	return strings.TrimSuffix(m.Prefix, "/") + "/" + strings.TrimPrefix(path, "/")
}

func (m *MinioStorageImpl) Put(ctx context.Context, data []byte, path string) error {
	log := logger.FromCtx(ctx)
	log.Info("uploading to minio", zap.String("path", m.objectKey(path)))
	_, err := m.Client.PutObject(ctx, m.Bucket, m.objectKey(path), bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})

	if err != nil {
		log.Error("error uploading to minio", zap.String("path", path), zap.String("error", err.Error()))
	}
	return err
}

func (m *MinioStorageImpl) Get(ctx context.Context, path string) ([]byte, error) {
	object, err := m.Client.GetObject(ctx, m.Bucket, m.objectKey(path), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(object)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil

}

func (m *MinioStorageImpl) Delete(ctx context.Context, path string) error {
	return m.Client.RemoveObject(ctx, m.Bucket, m.objectKey(path), minio.RemoveObjectOptions{})
}

// localPath is the inverse of objectKey: turns a bucket key back into the
// leading-slashed path the rest of the service passes around.
func (m *MinioStorageImpl) localPath(key string) string {
	if m.Prefix != "" {
		key = strings.TrimPrefix(key, strings.TrimSuffix(m.Prefix, "/")+"/")
	}
	return "/" + key
}

func (m *MinioStorageImpl) List(ctx context.Context, path string, recursive bool) <-chan storage.Entry {
	out := make(chan storage.Entry)

	// objectKey insists on a leading slash to strip; for a listing prefix an
	// empty path means "the whole prefix", which is a legitimate input.
	prefix := strings.TrimPrefix(m.objectKey(path), "/")

	go func() {
		defer close(out)
		for obj := range m.Client.ListObjects(ctx, m.Bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: recursive,
		}) {
			if obj.Err != nil {
				select {
				case out <- storage.Entry{Err: obj.Err}:
				case <-ctx.Done():
				}
				return
			}
			// Non-recursive listings surface pseudo-directories as keys with a
			// trailing slash. They are not objects, and following them is what
			// the recursive flag is for.
			if strings.HasSuffix(obj.Key, "/") {
				continue
			}
			select {
			case out <- storage.Entry{Path: m.localPath(obj.Key)}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func (m *MinioStorageImpl) Copy(ctx context.Context, srcPath, dstPath string) error {
	_, err := m.Client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: m.Bucket, Object: m.objectKey(dstPath)},
		minio.CopySrcOptions{Bucket: m.Bucket, Object: m.objectKey(srcPath)},
	)
	return err
}

func (m *MinioStorageImpl) Exists(ctx context.Context, path string) (bool, error) {
	_, err := m.Client.StatObject(ctx, m.Bucket, m.objectKey(path), minio.StatObjectOptions{})
	if err != nil {
		// HEAD has no body to carry an error code, so S3 implementations
		// disagree on what they put here — the status is the reliable signal.
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
