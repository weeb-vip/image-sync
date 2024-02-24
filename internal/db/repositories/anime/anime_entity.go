package anime

import (
	"database/sql"
	"time"
)

type Anime struct {
	ID            string       `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	AnidbID       *string      `gorm:"column:anidbid;null" json:"anidbid"`
	Type          *RECORD_TYPE `gorm:"column:type;type:text;default:Anime" json:"type"`
	TitleEn       *string      `gorm:"column:title_en;null" json:"title_en"`
	TitleJp       *string      `gorm:"column:title_jp;null" json:"title_jp"`
	TitleRomaji   *string      `gorm:"column:title_romaji;null" json:"title_romaji"`
	TitleKanji    *string      `gorm:"column:title_kanji;null" json:"title_kanji"`
	TitleSynonyms *string      `gorm:"column:title_synonyms;type:text;null" json:"title_synonyms"`
	ImageURL      *string      `gorm:"column:image_url;null" json:"image_url"`
	Synopsis      *string      `gorm:"column:synopsis;null" json:"synopsis"`
	Episodes      *int         `gorm:"column:episodes;null" json:"episodes"`
	Status        *string      `gorm:"column:status;null" json:"status"`
	StartDate     sql.NullTime `gorm:"column:start_date;type:date;null" json:"start_date"`
	EndDate       sql.NullTime `gorm:"column:end_date;type:date;null" json:"end_date"`
	Genres        *string      `gorm:"column:genres;type:text;null" json:"genres"`
	Duration      *string      `gorm:"column:duration;null" json:"duration"`
	Broadcast     *string      `gorm:"column:broadcast;null" json:"broadcast"`
	Source        *string      `gorm:"column:source;null" json:"source"`
	Licensors     *string      `gorm:"column:licensors;type:text;null" json:"licensors"`
	Studios       *string      `gorm:"column:studios;type:text;null" json:"studios"`
	Rating        *string      `gorm:"column:rating;null" json:"rating"`
	Ranking       *int         `gorm:"column:ranking;null" json:"ranking"`
	CreatedAt     time.Time    `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time    `gorm:"column:updated_at" json:"updated_at"`
}

// set table name
func (Anime) TableName() string {
	return "anime"
}
