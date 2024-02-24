package config

import (
	"github.com/jinzhu/configor"
)

type Config struct {
	AppConfig    AppConfig
	DBConfig     DBConfig
	PulsarConfig PulsarConfig
	MinioConfig  MinioConfig
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
	Port     uint   `default:"3306" env:"DBPORT"`
}

type PulsarConfig struct {
	URL              string `default:"pulsar://localhost:6650" env:"PULSARURL"`
	Topic            string `default:"public/default/myanimelist.public.anime" env:"PULSARTOPIC"`
	SubscribtionName string `default:"my-sub" env:"PULSARSUBSCRIPTIONNAME"`
}

type MinioConfig struct {
	Endpoint        string `default:"localhost:9000" env:"MINIO_ENDPOINT"`
	AccessKeyID     string `default:"minio" env:"MINIO_ACCESS_KEY_ID"`
	SecretAccessKey string `default:"minio123" env:"MINIO_SECRET_ACCESS_KEY"`
	UseSSL          bool   `default:"false" env:"MINIO_USESSL"`
	Bucket          string `default:"anime" env:"MINIO_BUCKET"`
}

func LoadConfigOrPanic() Config {
	var config = Config{}
	configor.Load(&config, "config/config.dev.json")

	return config
}
