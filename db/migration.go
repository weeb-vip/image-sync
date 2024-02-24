package db

import (
	"embed"
	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
	_ "github.com/golang-migrate/migrate/v4/source/httpfs"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/db"
	"net/http"
)

var (
	//go:embed migrations/*.sql
	migrations embed.FS
)

type driver struct {
	httpfs.PartialDriver
}

func (d *driver) Open(rawURL string) (source.Driver, error) {
	err := d.PartialDriver.Init(http.FS(migrations), "migrations")
	if err != nil {
		return nil, err
	}

	return d, nil
}
func getMigration() (*migrate.Migrate, error) {
	cfg := config.LoadConfigOrPanic()
	database := db.NewDB(cfg.DBConfig)
	sqldb, err := database.DB.DB()
	if err != nil {
		return nil, err
	}
	dbdriver, err := mysql.WithInstance(sqldb, &mysql.Config{})
	// log files in migrations folder
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		println(file.Name())
	}

	source.Register("embed", &driver{})

	return migrate.NewWithDatabaseInstance("embed://", cfg.DBConfig.DataBase, dbdriver)
}

func MigrateUp() error {
	m, err := getMigration()
	if err != nil {
		return err
	}

	return m.Up()
}

func MigrateDown() error {
	m, err := getMigration()
	if err != nil {
		return err
	}

	return m.Down()
}
