package eventing

import (
	"context"
	"github.com/ThatCatDev/ep/v2/drivers"
	epNats "github.com/ThatCatDev/ep/v2/drivers/nats"
	"github.com/ThatCatDev/ep/v2/middlewares/nats/backoffretry"
	"github.com/ThatCatDev/ep/v2/processor"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/middlewares/workerpool"
	"github.com/weeb-vip/image-sync/internal/services/image_processor"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
)

// EventingImageNats is the NATS counterpart of EventingImageKafka.
//
// A separate entry point rather than a flag, so production keeps running the
// Kafka command untouched while staging moves over. The processor, the worker
// pool and the retry policy are shared; only the transport differs.
//
// There is no transform middleware here, and none is needed. ep's processor
// hands the raw body to Event.Transform, and both drivers pass it the same way
// -- Kafka gives it msg.Value, NATS gives it msg.Data -- so Payload is
// populated identically. The CDC consumers need a transform only because their
// bodies are wrapped in a Debezium {schema, payload} envelope; image-sync
// messages are the sync services' own JSON.
func EventingImageNats() error {
	cfg := config.LoadConfigOrPanic()
	ctx := context.Background()
	log := logger.Get()
	ctx = logger.WithCtx(ctx, log)

	store := minio.NewMinioStorage(cfg.MinioConfig)

	natsConfig := &epNats.Config{
		URL:               cfg.NatsConfig.URL,
		ConsumerGroupName: cfg.NatsConfig.ConsumerGroupName,
		// Empty StreamName: image-sync is not CDC, so the driver creates a
		// stream from the subject rather than binding to Debezium's.
		StreamName:              cfg.NatsConfig.StreamName,
		ConsumerAutoOffsetReset: &cfg.NatsConfig.Offset,
	}

	log.Info("Creating NATS driver", zap.String("url", cfg.NatsConfig.URL))
	driver := epNats.NewNatsDriver(natsConfig)
	defer func(driver drivers.Driver[*epNats.Message]) {
		err := driver.Close()
		if err != nil {
			log.Error("Error closing NATS driver", zap.String("error", err.Error()))
		} else {
			log.Info("NATS driver closed successfully")
		}
	}(driver)

	log.Info("Creating processor for NATS messages", zap.String("subject", cfg.NatsConfig.Subject))
	imageProcessor := image_processor.NewImageProcessor[*epNats.Message](store)

	// Create worker pool middleware
	workerPoolMiddleware := workerpool.NewWorkerPoolMiddleware[*epNats.Message, image_processor.Payload](driver, workerpool.Config{
		Workers:    cfg.WorkerConfig.KafkaImageProcessorWorkers,
		BufferSize: cfg.WorkerConfig.BufferSize,
	})
	workerPoolMiddleware.SetNextProcessor(imageProcessor.Process)
	workerPoolMiddleware.Start(ctx)
	defer workerPoolMiddleware.Stop()

	log.Info("Started NATS worker pool middleware", zap.Int("workers", cfg.WorkerConfig.KafkaImageProcessorWorkers), zap.Int("bufferSize", cfg.WorkerConfig.BufferSize))

	processorInstance := processor.NewProcessor[*epNats.Message, image_processor.Payload](driver, cfg.NatsConfig.Subject, workerPoolMiddleware.Process)

	log.Info("initializing backoff retry middleware", zap.String("subject", cfg.NatsConfig.Subject))
	backoffRetryInstance := backoffretry.NewBackoffRetry[image_processor.Payload](driver, backoffretry.Config{
		MaxRetries: 3,
		HeaderKey:  "retry",
		RetryQueue: cfg.NatsConfig.Subject + "-retry",
	})

	log.Info("Starting NATS processor", zap.String("subject", cfg.NatsConfig.Subject))
	err := processorInstance.
		AddMiddleware(backoffRetryInstance.Process).
		Run(ctx)

	if err != nil && ctx.Err() == nil { // Ignore error if caused by context cancellation
		log.Error("Error consuming messages", zap.String("error", err.Error()))
		return err
	}

	return nil
}
