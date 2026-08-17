package image_backfill

import (
	"context"
	"database/sql"
	"net/url"

	"github.com/weeb-vip/image-sync/internal/logger"
	"go.uber.org/zap"
)

// MissingAnime is one anime with no poster in the bucket, carrying enough to
// go and fetch one again.
type MissingAnime struct {
	ID      string  `json:"id"`
	AnidbID *string `json:"anidbid,omitempty"`
	TitleEn *string `json:"title_en,omitempty"`
	TitleJp *string `json:"title_jp,omitempty"`
	// ImageURL is the source address the sync services were given. Present here
	// means the record knows where its art lives and the copy into the bucket
	// is what failed; absent means there is nothing to fetch and the record
	// itself needs rescraping. The two want different follow-ups, so the
	// distinction is kept rather than flattened.
	ImageURL *string `json:"image_url,omitempty"`
}

type MissingReport struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	// Total is every anime row considered.
	Total int `json:"total_anime"`
	// WithImage counts rows resolving to an object under either key.
	WithImage int `json:"with_image"`
	// WithoutImage is len(Anime), repeated here so the counts read together.
	WithoutImage int `json:"without_image"`
	// WithoutImageURL is the subset that has no source address either — the
	// ones a rescrape has to start from scratch on.
	WithoutImageURL int            `json:"without_image_url"`
	Anime           []MissingAnime `json:"anime"`
}

// MissingImages reports every anime with no poster in the bucket.
//
// One listing, not a HEAD per anime: at ~32,000 rows the per-object approach is
// half an hour of round trips, while the whole root prefix lists in seconds.
//
// An anime counts as covered if an object exists under its id *or* under its
// legacy title slug, since the backfill deliberately leaves some records on the
// old key — where two anime shared a title it cannot tell whose object it is.
// Those still render in the frontend, so calling them missing would send you
// rescraping art that is already there.
func (b *Backfiller) MissingImages(ctx context.Context) (*MissingReport, error) {
	log := logger.FromCtx(ctx)

	log.Info("listing bucket")
	present := map[string]struct{}{}
	for entry := range b.Storage.List(ctx, "/", false) {
		if entry.Err != nil {
			return nil, entry.Err
		}
		present[basename(entry.Path)] = struct{}{}
	}
	log.Info("bucket listed", zap.Int("objects", len(present)))

	report := &MissingReport{Anime: []MissingAnime{}}

	err := b.scanAll(ctx, "anime", "id, anidbid, title_en, title_jp, image_url", func(rows *sql.Rows) (string, error) {
		var (
			id       string
			anidbID  *string
			titleEn  *string
			titleJp  *string
			imageURL *string
		)
		if err := rows.Scan(&id, &anidbID, &titleEn, &titleJp, &imageURL); err != nil {
			return "", err
		}
		report.Total++

		if _, ok := present[url.QueryEscape(id)]; ok {
			report.WithImage++
			return id, nil
		}
		for _, t := range []*string{titleEn, titleJp} {
			if t == nil {
				continue
			}
			if _, ok := present[titleSlug(*t)]; ok {
				report.WithImage++
				return id, nil
			}
		}

		if imageURL == nil || *imageURL == "" {
			report.WithoutImageURL++
		}
		report.Anime = append(report.Anime, MissingAnime{
			ID:       id,
			AnidbID:  anidbID,
			TitleEn:  titleEn,
			TitleJp:  titleJp,
			ImageURL: imageURL,
		})
		return id, nil
	})
	if err != nil {
		return nil, err
	}

	report.WithoutImage = len(report.Anime)
	log.Info("scan complete",
		zap.Int("total", report.Total),
		zap.Int("withImage", report.WithImage),
		zap.Int("withoutImage", report.WithoutImage),
		zap.Int("withoutImageURL", report.WithoutImageURL))

	return report, nil
}
