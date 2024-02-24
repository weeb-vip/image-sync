package anime

import "github.com/weeb-vip/image-sync/internal/db"

type RECORD_TYPE string

type AnimeEpisodeRepositoryImpl interface {
	Upsert(anime *AnimeEpisode) error
	Delete(anime *AnimeEpisode) error
}

type AnimeEpisodeRepository struct {
	db *db.DB
}

func NewAnimeRepository(db *db.DB) AnimeEpisodeRepositoryImpl {
	return &AnimeEpisodeRepository{db: db}
}

func (a *AnimeEpisodeRepository) Upsert(episode *AnimeEpisode) error {
	err := a.db.DB.Save(episode).Error
	if err != nil {
		return err
	}
	return nil
}

func (a *AnimeEpisodeRepository) Delete(episode *AnimeEpisode) error {
	err := a.db.DB.Delete(episode).Error
	if err != nil {
		return err
	}
	return nil
}
