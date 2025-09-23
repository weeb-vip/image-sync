package eventing

import (
	"context"
	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/image_processor"
	"github.com/weeb-vip/image-sync/internal/services/processor"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"github.com/weeb-vip/image-sync/internal/worker"
	"go.uber.org/zap"
)

func EventingImage() error {
	cfg := config.LoadConfigOrPanic()
	ctx := context.Background()
	log := logger.Get()

	store := minio.NewMinioStorage(cfg.MinioConfig)

	imageProcessor := image_processor.NewImageProcessor(store)

	messageProcessor := processor.NewProcessor[image_processor.Payload]()

	// Create worker pool for concurrent image processing
	workerPool := worker.NewPool(cfg.WorkerConfig.ImageProcessorWorkers, cfg.WorkerConfig.BufferSize, func(ctx context.Context, payload image_processor.Payload) error {
		return imageProcessor.Process(ctx, payload)
	})

	// Start the worker pool
	workerPool.Start(ctx)
	defer workerPool.Stop()

	log.Info("Started worker pool", zap.Int("workers", cfg.WorkerConfig.ImageProcessorWorkers), zap.Int("bufferSize", cfg.WorkerConfig.BufferSize))

	client, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: cfg.PulsarConfig.URL,
	})

	if err != nil {
		log.Fatal("Error creating pulsar client: ", zap.String("error", err.Error()))
		return err
	}

	defer client.Close()

	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            cfg.PulsarConfig.Topic,
		SubscriptionName: cfg.PulsarConfig.SubscribtionName,
		Type:             pulsar.Shared,
	})

	defer consumer.Close()
	ctx = logger.WithCtx(ctx, log)

	// create channel to receive messages

	for {
		msg, err := consumer.Receive(ctx)
		if err != nil {
			log.Fatal("Error receiving message: ", zap.String("error", err.Error()))
		}

		log.Info("Received message", zap.String("msgId", msg.ID().String()))

		// Parse the message payload to get the data structure
		payload, err := messageProcessor.Parse(ctx, string(msg.Payload()))
		if err != nil {
			log.Warn("error parsing message payload: ", zap.String("error", err.Error()))
			consumer.Ack(msg)
			continue
		}

		// Submit job to worker pool
		done := workerPool.Submit(*payload)

		// Wait for processing to complete
		go func(msg pulsar.Message, done chan error) {
			err := <-done
			if err != nil {
				log.Warn("error processing message: ", zap.String("error", err.Error()))
			}
			consumer.Ack(msg)
		}(msg, done)
	}

}
