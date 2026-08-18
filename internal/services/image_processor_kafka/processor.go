package image_processor

import (
	"github.com/ThatCatDev/ep/v2/event"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/imagepath"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
	"golang.org/x/net/context"
	"io"
	"net/http"
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

	if dataPayload.URL == "" {
		log.Warn("skipping message with empty image url", zap.Any("payload", data.Payload))
		return data, nil
	}

	path, ok := imagepath.For(dataPayload.Type, dataPayload.ID, dataPayload.Name)
	if !ok {
		log.Warn("skipping message with no storable path", zap.Any("payload", data.Payload))
		return data, nil
	}

	// A message arrives on every anime update, not only when the artwork
	// changes, so the image we already hold is usually the one being offered
	// again. Comparing the stored size against the source's Content-Length
	// settles that with one small request instead of refetching the whole
	// file -- and unlike skipping whenever the object exists, genuinely new
	// artwork still gets picked up, because its size differs.
	//
	// This matters in bulk: a backfill touching every anime row would
	// otherwise pull ~30,000 images from MyAnimeList that we already have.
	if unchanged(ctx, p.Storage, path, dataPayload.URL) {
		log.Info("image already stored and unchanged, skipping download",
			zap.String("path", path), zap.String("url", dataPayload.URL))
		return data, nil
	}

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

	err = p.Storage.Put(ctx, imageData, path)
	if err != nil {
		log.Error("error uploading image to storage", zap.String("error", err.Error()))
		return data, err
	}
	log.Info("image processing complete", zap.String("path", path))
	return data, nil

}

// unchanged reports whether the stored object already matches the source.
//
// Deliberately conservative: any uncertainty -- a stat error, a source that
// does not report a length, a HEAD it will not answer -- returns false, so the
// image is fetched. The cost of being wrong here is one redundant download,
// against silently serving stale artwork forever.
func unchanged(ctx context.Context, store storage.Storage, path, src string) bool {
	size, found, err := store.Stat(ctx, path)
	if err != nil || !found || size <= 0 {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, src, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return false
	}

	return resp.ContentLength == size
}
