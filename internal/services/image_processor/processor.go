package image_processor

import (
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"io"
	"net/http"
	"net/url"
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
	log.Info("After", zap.Any("payload", data.Data))

	// download image
	log.Info("downloading image", zap.String("url", data.Data.URL))
	resp, err := http.Get(data.Data.URL)
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
	// convert title_en to lowercase and replace spaces with underscores

	name := url.QueryEscape(data.Data.Name)
	if data.Data.Type == DataTypeAnime {

	} else if data.Data.Type == DataTypeCharacter {
		name = "characters/" + name
	} else if data.Data.Type == DataTypeStaff {
		name = "staff/" + name
	} else {
		return nil
	}
	err = p.Storage.Put(ctx, imageData, "/"+name)
	if err != nil {
		return err
	}
	log.Info("image processing complete (did not save image)")
	return nil

}
