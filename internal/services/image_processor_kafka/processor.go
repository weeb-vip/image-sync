package image_processor

import (
	"github.com/ThatCatDev/ep/v2/event"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"io"
	"net/http"
	"net/url"
)

type ImageProcessor interface {
	Process(ctx context.Context, data event.Event[*kafka.Message, Payload]) (event.Event[*kafka.Message, Payload], error)
}

type ImageProcessorImpl struct {
	Storage storage.Storage
}

func NewImageProcessor(store storage.Storage) ImageProcessor {
	return &ImageProcessorImpl{
		Storage: store,
	}
}

func (p *ImageProcessorImpl) Process(ctx context.Context, data event.Event[*kafka.Message, Payload]) (event.Event[*kafka.Message, Payload], error) {
	log := logger.FromCtx(ctx)

	dataPayload := data.Payload.Data
	log.Info("New record")
	// new record
	// log after payload
	log.Info("Got message", zap.Any("payload", data.Payload))

	// download image
	log.Info("downloading image", zap.String("url", dataPayload.URL))
	resp, err := http.Get(dataPayload.URL)
	if err != nil {
		return data, err
	}
	defer resp.Body.Close()
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return data, err
	}

	// save to storage
	log.Info("uploading image to storage")
	// convert title_en to lowercase and replace spaces with underscores

	name := url.QueryEscape(dataPayload.Name)

	var dataType DataType
	dataType = dataPayload.Type
	if dataType == DataTypeAnime {

	} else if dataType == DataTypeCharacter {
		name = "characters/" + name
	} else if dataType == DataTypeStaff {
		name = "staff/" + name
	} else {
		return data, nil
	}
	err = p.Storage.Put(ctx, imageData, "/"+name)
	if err != nil {
		log.Error("error uploading image to storage", zap.String("error", err.Error()))
		return data, err
	}
	log.Info("image processing complete (did not save image)")
	return data, nil

}
