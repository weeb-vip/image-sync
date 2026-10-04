package config

import (
	"github.com/jinzhu/configor"
)

type Config struct {
	AppConfig      AppConfig
	DBConfig       DBConfig
	PostgresConfig PostgresConfig
	MinioConfig    MinioConfig
	KafkaConfig    KafkaConfig
	NatsConfig     NatsConfig
	WorkerConfig   WorkerConfig
}

type AppConfig struct {
	APPName string `default:"anime-api"`
	Port    int    `env:"PORT" default:"3000"`
	Version string `default:"x.x.x"`
}

type DBConfig struct {
	Host     string `default:"localhost" env:"DBHOST"`
	DataBase string `default:"weeb" env:"DBNAME"`
	User     string `default:"weeb" env:"DBUSERNAME"`
	Password string `required:"true" env:"DBPASSWORD" default:"mysecretpassword"`
	Port     uint   `default:"5432" env:"DBPORT"`
	SSLMode  string `default:"require" env:"DBSSL"`
}

// PostgresConfig points at the scraper's source database. Only the rescrape
// report uses it: that is where mal_id lives, and the MySQL side has no
// equivalent column to build a MyAnimeList URL from.
type PostgresConfig struct {
	Host     string `default:"localhost" env:"PGHOST"`
	DataBase string `default:"anime" env:"PGDATABASE"`
	User     string `default:"postgres" env:"PGUSER"`
	Password string `default:"mysecretpassword" env:"PGPASSWORD"`
	Port     uint   `default:"5432" env:"PGPORT"`
	SSLMode  string `default:"disable" env:"PGSSLMODE"`
}

type MinioConfig struct {
	Endpoint        string `default:"localhost:9000" env:"MINIO_ENDPOINT"`
	AccessKeyID     string `default:"minio" env:"MINIO_ACCESS_KEY_ID"`
	SecretAccessKey string `default:"minio123" env:"MINIO_SECRET_ACCESS_KEY"`
	UseSSL          bool   `default:"false" env:"MINIO_USESSL"`
	Bucket          string `default:"anime" env:"MINIO_BUCKET"`
	Prefix          string `default:"" env:"MINIO_PREFIX"`
}

type KafkaConfig struct {
	ConsumerGroupName string `default:"image-sync-group" env:"KAFKA_CONSUMER_GROUP_NAME"`
	BootstrapServers  string `default:"localhost:9092" env:"KAFKA_BOOTSTRAP_SERVERS"`
	Topic             string `default:"image-sync-topic" env:"KAFKA_TOPIC"`
	Offset            string `default:"earliest" env:"KAFKA_OFFSET"`
	Debug             string `default:"" env:"KAFKA_DEBUG"`
}

type WorkerConfig struct {
	ImageProcessorWorkers      int `default:"4" env:"WORKER_IMAGE_PROCESSOR_COUNT"`
	KafkaImageProcessorWorkers int `default:"4" env:"WORKER_KAFKA_IMAGE_PROCESSOR_COUNT"`
	BufferSize                 int `default:"100" env:"WORKER_BUFFER_SIZE"`
}

// NatsConfig mirrors KafkaConfig, so moving between the two is one substitution
// per setting.
type NatsConfig struct {
	URL string `default:"nats://localhost:4222" env:"NATSURL"`

	// The durable consumer name -- the closest equivalent to a Kafka consumer
	// group. Left empty the consumer is ephemeral and loses its position on
	// restart.
	ConsumerGroupName string `default:"image-sync-nats" env:"NATSCONSUMERGROUPNAME"`

	// Empty on purpose, unlike the CDC consumers.
	//
	// image-sync is produced by the sync services, not by Debezium, so no other
	// stream declares it and the driver should create one from the subject.
	// Naming Debezium's stream here would try to graft image-sync onto the CDC
	// stream, whose retention is sized for change events rather than this.
	StreamName string `env:"NATSSTREAMNAME"`

	Offset string `default:"earliest" env:"NATSOFFSET"`

	Subject string `default:"image-sync" env:"NATSSUBJECT"`

	// Where a stored object is announced for whoever wants to post-process it
	// (upscaler-service). Empty turns the announcement off.
	StoredSubject string `default:"image-stored" env:"NATS_STORED_SUBJECT"`
}

func LoadConfigOrPanic() Config {
	var config = Config{}
	configor.Load(&config, "config/config.dev.json")

	return config
}
