package db

import (
	"fmt"
	"github.com/weeb-vip/image-sync/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"time"
)

type DB struct {
	DB *gorm.DB
}

func NewDB(cfg config.DBConfig) *DB {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DataBase)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get database connection")
	}

	// Set maximum number of open connections
	// This prevents too many connections to the database
	sqlDB.SetMaxOpenConns(25)

	// Set maximum number of idle connections
	// This maintains a pool of reusable connections
	sqlDB.SetMaxIdleConns(10)

	// Set maximum lifetime of a connection
	// MySQL wait_timeout is typically 8 hours, so we set this lower
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Set maximum idle time for a connection
	// This helps clean up idle connections
	sqlDB.SetConnMaxIdleTime(90 * time.Second)

	return &DB{DB: db}
}
