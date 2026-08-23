package db

import (
	"fmt"
	"github.com/weeb-vip/image-sync/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"time"
)

type DB struct {
	DB *gorm.DB
}

func NewDB(cfg config.DBConfig) *DB {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DataBase, cfg.SSLMode)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get database connection")
	}

	// The pool is sized to be reused rather than refilled.
	//
	// MaxIdleConns matches MaxOpenConns deliberately: Go only retains up to
	// MaxIdleConns, so anything opened above it is closed again the moment the
	// query finishes -- TCP, TLS and auth paid per query rather than once.
	//
	// 2 because this is a Kafka consumer: it processes one message at a time,
	// so the write concurrency is one plus a spare. The previous 25 could never
	// be used, but counted against a database that allows 79 connections in
	// total across roughly 36 pods.
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)

	// Long enough that connections survive quiet periods and get reused, short
	// enough that a failover or DNS change is picked up without a restart.
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return &DB{DB: db}
}

// NewPostgresDB connects to the scraper's source database, which the rescrape
// report reads mal_id from.
func NewPostgresDB(cfg config.PostgresConfig) *DB {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DataBase, cfg.SSLMode)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect postgres: " + err.Error())
	}
	return &DB{DB: db}
}
