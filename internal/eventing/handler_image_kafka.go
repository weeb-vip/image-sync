package eventing

import (
	"context"
	"github.com/ThatCatDev/ep/v2/drivers"
	epKafka "github.com/ThatCatDev/ep/v2/drivers/kafka"
	"github.com/ThatCatDev/ep/v2/middlewares/kafka/backoffretry"
	"github.com/ThatCatDev/ep/v2/processor"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/middlewares/workerpool"
	"github.com/weeb-vip/image-sync/internal/services/image_processor_kafka"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
)

func EventingImageKafka() error {
	cfg := config.LoadConfigOrPanic()
	ctx := context.Background()
	log := logger.Get()
	ctx = logger.WithCtx(ctx, log)

	store := minio.NewMinioStorage(cfg.MinioConfig)

	debug := &cfg.KafkaConfig.Debug
	if *debug == "" {
		debug = nil
	}
	kafkaConfig := &epKafka.KafkaConfig{
		ConsumerGroupName:        cfg.KafkaConfig.ConsumerGroupName,
		BootstrapServers:         cfg.KafkaConfig.BootstrapServers,
		SaslMechanism:            nil,
		SecurityProtocol:         nil,
		Username:                 nil,
		Password:                 nil,
		ConsumerSessionTimeoutMs: nil,
		ConsumerAutoOffsetReset:  &cfg.KafkaConfig.Offset,
		ClientID:                 nil,
		Debug:                    debug,
	}

	log.Info("Creating Kafka driver", zap.String("bootstrapServers", cfg.KafkaConfig.BootstrapServers))
	driver := epKafka.NewKafkaDriver(kafkaConfig)
	defer func(driver drivers.Driver[*kafka.Message]) {
		err := driver.Close()
		if err != nil {
			log.Error("Error closing Kafka driver", zap.String("error", err.Error()))
		} else {
			log.Info("Kafka driver closed successfully")
		}
	}(driver)

	log.Info("Creating processor for Kafka messages", zap.String("topic", cfg.KafkaConfig.Topic))
	imageProcessor := image_processor.NewImageProcessor(store)

	// Create worker pool middleware
	workerPoolMiddleware := workerpool.NewWorkerPoolMiddleware[*kafka.Message, image_processor.Payload](driver, workerpool.Config{
		Workers:    cfg.WorkerConfig.KafkaImageProcessorWorkers,
		BufferSize: cfg.WorkerConfig.BufferSize,
	})
	workerPoolMiddleware.SetNextProcessor(imageProcessor.Process)
	workerPoolMiddleware.Start(ctx)
	defer workerPoolMiddleware.Stop()

	log.Info("Started Kafka worker pool middleware", zap.Int("workers", cfg.WorkerConfig.KafkaImageProcessorWorkers), zap.Int("bufferSize", cfg.WorkerConfig.BufferSize))

	processorInstance := processor.NewProcessor[*kafka.Message, image_processor.Payload](driver, cfg.KafkaConfig.Topic, workerPoolMiddleware.Process)

	log.Info("initializing backoff retry middleware", zap.String("topic", cfg.KafkaConfig.Topic))
	backoffRetryInstance := backoffretry.NewBackoffRetry[image_processor.Payload](driver, backoffretry.Config{
		MaxRetries: 3,
		HeaderKey:  "retry",
		RetryQueue: cfg.KafkaConfig.Topic + "-retry",
	})

	log.Info("Starting Kafka processor", zap.String("topic", cfg.KafkaConfig.Topic))
	err := processorInstance.
		AddMiddleware(backoffRetryInstance.Process).
		Run(ctx)

	if err != nil && ctx.Err() == nil { // Ignore error if caused by context cancellation
		log.Error("Error consuming messages", zap.String("error", err.Error()))
		return err
	}

	return nil
}
