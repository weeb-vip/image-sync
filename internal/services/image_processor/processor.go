package image_processor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ThatCatDev/ep/v2/event"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/imagepath"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
	"golang.org/x/net/context"
)

// Publish sends one encoded event; nil means nobody is listening.
type Publish func(ctx context.Context, value []byte) error

// StoredEvent is what goes out on the image-stored subject once an object
// has been written: enough for a consumer (the upscaler) to find the object
// and decide whether it wants it, without re-reading the source.
type StoredEvent struct {
	// Path is bucket-prefix-relative and leading-slashed, as Storage takes it.
	Path        string `json:"path"`
	Type        string `json:"type"`
	ID          string `json:"id"`
	SourceURL   string `json:"source_url"`
	Size        int    `json:"size"`
	ContentType string `json:"content_type"`
}

// The driver message type is a parameter because the processor never looks at
// it. Nothing here reads DriverMessage, RawData or Headers -- only Payload,
// which the transform middleware has already filled in. This package was called
// image_processor_kafka purely because *kafka.Message was baked into the
// signature; none of the logic was ever Kafka-specific.
type ImageProcessor[DM any] interface {
	Process(ctx context.Context, data event.Event[DM, Payload]) (event.Event[DM, Payload], error)
	// Refresh stores an image again whether or not the source has changed:
	// the deliberate re-pull, for art that should be fetched and announced
	// anew (a refreshed object carries no upscale provenance, so the
	// upscaler redoes it from the larger source).
	Refresh(ctx context.Context, image ImageSchema) error
}

type ImageProcessorImpl[DM any] struct {
	Storage storage.Storage
	// Publish announces a stored object; nil publishes nothing. A failure to
	// announce is logged and swallowed: the image is stored either way, and
	// the announcement is for a consumer that may not be deployed.
	Publish Publish
}

func NewImageProcessor[DM any](store storage.Storage, publish Publish) ImageProcessor[DM] {
	return &ImageProcessorImpl[DM]{
		Storage: store,
		Publish: publish,
	}
}

func (p *ImageProcessorImpl[DM]) Process(ctx context.Context, data event.Event[DM, Payload]) (event.Event[DM, Payload], error) {
	log := logger.FromCtx(ctx)
	log.Info("New record")
	log.Info("Got message", zap.Any("payload", data.Payload))
	return data, p.store(ctx, data.Payload.Data, false)
}

func (p *ImageProcessorImpl[DM]) Refresh(ctx context.Context, image ImageSchema) error {
	return p.store(ctx, image, true)
}

// store fetches the image and writes it under its path. With force the
// unchanged check is skipped: the object is written and announced even when
// the bucket already holds exactly this source.
func (p *ImageProcessorImpl[DM]) store(ctx context.Context, dataPayload ImageSchema, force bool) error {
	log := logger.FromCtx(ctx)

	if dataPayload.URL == "" {
		log.Warn("skipping message with empty image url", zap.Any("image", dataPayload))
		return nil
	}

	path, ok := imagepath.For(dataPayload.Type, dataPayload.ID, dataPayload.Name)
	if !ok {
		log.Warn("skipping message with no storable path", zap.Any("image", dataPayload))
		return nil
	}

	// The larger copy where the source has one. Decided before the skip
	// rule, so an object stored from the small copy is replaced once by the
	// large one (its recorded source length differs) and then left alone.
	if large := preferredSource(ctx, dataPayload.URL); large != dataPayload.URL {
		log.Info("using the larger source", zap.String("url", large))
		dataPayload.URL = large
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
	if !force && unchanged(ctx, p.Storage, path, dataPayload.URL) {
		log.Info("image already stored and unchanged, skipping download",
			zap.String("path", path), zap.String("url", dataPayload.URL))
		return nil
	}

	// download image
	log.Info("downloading image", zap.String("url", dataPayload.URL))
	resp, err := http.Get(dataPayload.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: %s", dataPayload.URL, resp.Status)
	}
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// save to storage, with the real content type (it used to be
	// octet-stream for everything) and the source's length, which is what
	// `unchanged` compares against from now on.
	contentType := http.DetectContentType(imageData)
	meta := map[string]string{
		storage.MetaSourceLength: strconv.Itoa(len(imageData)),
		storage.MetaSourceURL:    dataPayload.URL,
	}
	log.Info("uploading image to storage", zap.String("contentType", contentType))
	err = p.Storage.PutObject(ctx, imageData, path, contentType, meta)
	if err != nil {
		log.Error("error uploading image to storage", zap.String("error", err.Error()))
		return err
	}
	log.Info("image processing complete", zap.String("path", path))

	p.announce(ctx, StoredEvent{
		Path: path, Type: dataPayload.Type, ID: dataPayload.ID, SourceURL: dataPayload.URL,
		Size: len(imageData), ContentType: contentType,
	})
	return nil
}

func (p *ImageProcessorImpl[DM]) announce(ctx context.Context, ev StoredEvent) {
	if p.Publish == nil {
		return
	}
	log := logger.FromCtx(ctx)
	body, err := json.Marshal(ev)
	if err != nil {
		log.Error("could not encode stored event", zap.Error(err))
		return
	}
	if err := p.Publish(ctx, body); err != nil {
		log.Warn("stored event not published; the object is stored regardless",
			zap.String("path", ev.Path), zap.Error(err))
	}
}

// unchanged reports whether the stored object already matches the source.
//
// Deliberately conservative: any uncertainty -- a stat error, a source that
// does not report a length, a HEAD it will not answer -- returns false, so the
// image is fetched. The cost of being wrong here is one redundant download,
// against silently serving stale artwork forever.
// unchanged: the object we hold came from a source of the same length as
// the one on offer. The stored source length wins where it exists; an object
// written before it was recorded falls back to its own size, which is right
// for an untouched download and wrong only for an object something has since
// rewritten -- those get the metadata on their next write.
func unchanged(ctx context.Context, store storage.Storage, path, src string) bool {
	info, found, err := store.Head(ctx, path)
	if err != nil || !found {
		return false
	}
	size := info.Size
	if recorded, err := strconv.ParseInt(info.Meta[storage.MetaSourceLength], 10, 64); err == nil && recorded > 0 {
		size = recorded
	}
	if size <= 0 {
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
