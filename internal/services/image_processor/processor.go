package image_processor

import (
	"github.com/weeb-vip/image-sync/internal/logger"
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
	if data.After != nil && data.After.TitleEn != nil {
		log = log.With(zap.String("animeName", *data.After.TitleEn))
	}
	if data.Before != nil && data.Before.TitleEn != nil {
		log = log.With(zap.String("animeName", *data.Before.TitleEn))
	}

	log.Info("processing image")

	if data.Before == nil && data.After != nil {
		log.Info("New record")
		// new record
		// log after payload
		log.Info("After", zap.Any("payload", data.After))
		if data.After.ImageUrl == nil {
			log.Info("no image to process")
			return nil
		}
		// download image
		log.Info("downloading image", zap.String("url", *data.After.ImageUrl))
		resp, err := http.Get(*data.After.ImageUrl)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		imageData, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		var file_name string
		if data.After.TitleEn != nil {
			file_name += *data.After.TitleEn + "_"
		}
		if data.After.TitleJp != nil {
			file_name += *data.After.TitleJp + "_"
		}

		file_name += data.After.Id
		// 166 characters from the end of the file name
		if len(file_name) > 166 {
			file_name = file_name[len(file_name)-166:]
		}
		// save to storage
		log.Info("uploading image to storage")
		err = p.Storage.Put(ctx, imageData, "/"+file_name)
		if err != nil {
			return err
		}
		return nil
	}

	//if data.Before != nil && data.After == nil {
	//	// new record
	//	if data.Before.ImageUrl == nil {
	//		return nil
	//	}
	//	// download image
	//	resp, err := http.Get(*data.Before.ImageUrl)
	//	if err != nil {
	//		return err
	//	}
	//	defer resp.Body.Close()
	//	imageData, err := io.ReadAll(resp.Body)
	//	if err != nil {
	//		return err
	//	}
	//
	//	// save to storage
	//	err = p.Storage.Put(ctx, imageData, data.Before.Id)
	//	if err != nil {
	//		return err
	//	}
	//
	//	return nil
	//}

	if data.Before != nil && data.After != nil {
		log.Info("Updating record")
		// new record
		if data.After.ImageUrl == nil {
			log.Info("no image to process")
			return nil
		}
		// download image
		log.Info("downloading image", zap.String("url", *data.After.ImageUrl))
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
		log.Info("uploading image to storage")

		var file_name string
		if data.After.TitleEn != nil {
			file_name += *data.After.TitleEn + "_"
		}
		if data.After.TitleJp != nil {
			file_name += *data.After.TitleJp + "_"
		}

		file_name += data.After.Id

		if len(file_name) > 166 {
			file_name = file_name[len(file_name)-166:]
		}
		err = p.Storage.Put(ctx, imageData, "/"+file_name)
		if err != nil {
			return err
		}
		return nil

	}
	log.Info("image processing complete (did not save image)")

	return nil
}
