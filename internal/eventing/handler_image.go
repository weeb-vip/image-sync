package eventing

import (
	"context"
	"fmt"
	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/services/image_processor"
	"github.com/weeb-vip/image-sync/internal/services/processor"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"

	"log"
	"time"
)

func EventingImage() error {
	cfg := config.LoadConfigOrPanic()
	ctx := context.Background()

	store := minio.NewMinioStorage(cfg.MinioConfig)

	imageProcessor := image_processor.NewImageProcessor(store)

	messageProcessor := processor.NewProcessor[image_processor.Payload]()

	client, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: cfg.PulsarConfig.URL,
	})

	if err != nil {
		log.Fatal(err)
		return err
	}

	defer client.Close()

	consumer, err := client.Subscribe(pulsar.ConsumerOptions{
		Topic:            cfg.PulsarConfig.Topic,
		SubscriptionName: cfg.PulsarConfig.SubscribtionName,
		Type:             pulsar.Shared,
	})

	defer consumer.Close()

	// create channel to receive messages

	for {
		msg, err := consumer.Receive(context.Background())
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Received message msgId: %#v -- content: '%s'\n",
			msg.ID(), string(msg.Payload()))

		err = messageProcessor.Process(ctx, string(msg.Payload()), imageProcessor.Process)
		if err != nil {
			log.Println("error processing message: ", err)
			continue
		}
		consumer.Ack(msg)
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}
