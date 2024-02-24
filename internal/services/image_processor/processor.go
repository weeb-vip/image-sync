package image_processor

import (
	"github.com/weeb-vip/image-sync/internal/services/storage"
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
	if data.Before == nil && data.After != nil {
		// new record
		if data.After.ImageUrl == nil {
			return nil
		}
		// download image
		resp, err := http.Get(*data.After.ImageUrl)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		imageData, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		// save to storage
		err = p.Storage.Put(ctx, imageData, data.After.Id)
		if err != nil {
			return err
		}
	}

	if data.Before != nil && data.After == nil {
		// new record
		if data.Before.ImageUrl == nil {
			return nil
		}
		// download image
		resp, err := http.Get(*data.Before.ImageUrl)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		imageData, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		// save to storage
		err = p.Storage.Put(ctx, imageData, data.Before.Id)
		if err != nil {
			return err
		}

		return nil
	}

	return nil
}
