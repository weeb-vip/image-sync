package image_processor

import (
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/imagepath"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"io"
	"net/http"
)

type ImageProcessor interface {
	Process(ctx context.Context, data Payload) error
}

type ImageProcessorImpl struct {
	Storage storage.Storage
}

func NewImageProcessor(store storage.Storage) ImageProcessor {
	return &ImageProcessorImpl{
		Storage: store,
	}
}

func (p *ImageProcessorImpl) Process(ctx context.Context, data Payload) error {
	log := logger.FromCtx(ctx)

	log.Info("New record")
	// new record
	// log after payload
	log.Info("After", zap.Any("payload", data))

	// download image
	log.Info("downloading image", zap.String("url", data.URL))
	resp, err := http.Get(data.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// save to storage
	log.Info("uploading image to storage")

	path, ok := imagepath.For(data.Type, data.ID, data.Name)
	if !ok {
		log.Warn("skipping message with no storable path", zap.Any("payload", data))
		return nil
	}

	err = p.Storage.Put(ctx, imageData, path)
	if err != nil {
		log.Error("error uploading image to storage", zap.String("error", err.Error()))
		return err
	}
	log.Info("image processing complete (did not save image)")
	return nil

}
