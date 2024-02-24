package anime

import (
	"database/sql"
	"time"
)

type AnimeEpisode struct {
	ID        string       `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	AnimeID   *string      `gorm:"column:anime_id;type:uuid;not null" json:"anime_id"`
	Episode   *int         `gorm:"column:episode;not null" json:"episode"`
	TitleEn   *string      `gorm:"column:title_en" json:"title_en"`
	TitleJp   *string      `gorm:"column:title_jp" json:"title_jp"`
	Aired     sql.NullTime `gorm:"column:aired;type:date" json:"aired"`
	Synopsis  *string      `gorm:"column:synopsis" json:"synopsis"`
	CreatedAt time.Time    `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time    `gorm:"column:updated_at" json:"updated_at"`
}

// set table name
func (AnimeEpisode) TableName() string {
	return "episodes"
}
