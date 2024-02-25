package minio

import (
	"bytes"
	"context"
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
	}
}

func (m *MinioStorageImpl) Put(ctx context.Context, data []byte, path string) error {
	log := logger.FromCtx(ctx)
	log.Info("uploading to minio", zap.String("path", path))
	_, err := m.Client.PutObject(ctx, m.Bucket, path, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})

	if err != nil {
		log.Error("error uploading to minio", zap.String("path", path), zap.String("error", err.Error()))
	}
	return err
}

func (m *MinioStorageImpl) Get(ctx context.Context, path string) ([]byte, error) {
	object, err := m.Client.GetObject(ctx, m.Bucket, path, minio.GetObjectOptions{})
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
	return m.Client.RemoveObject(ctx, m.Bucket, path, minio.RemoveObjectOptions{})
}
