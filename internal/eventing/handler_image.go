package eventing

import (
	"context"
	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/image_processor"
	"github.com/weeb-vip/image-sync/internal/services/processor"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
	"time"
)

func EventingImage() error {
	cfg := config.LoadConfigOrPanic()
	ctx := context.Background()
	log := logger.Get()

	store := minio.NewMinioStorage(cfg.MinioConfig)

	imageProcessor := image_processor.NewImageProcessor(store)

	messageProcessor := processor.NewProcessor[image_processor.Payload]()

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

		err = messageProcessor.Process(ctx, string(msg.Payload()), imageProcessor.Process)
		if err != nil {
			log.Warn("error processing message: ", zap.String("error", err.Error()))
			consumer.Ack(msg)
			continue
		}
		consumer.Ack(msg)
		time.Sleep(50 * time.Millisecond)
	}

}
