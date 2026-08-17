package image_backfill

import (
	"context"
	"fmt"
	"net/url"

	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
)

// RescrapeTarget is one anime with no image in the bucket, and where to go and
// fetch it from again.
type RescrapeTarget struct {
	ID      string `json:"id"`
	TitleEn string `json:"title_en,omitempty"`
	TitleJp string `json:"title_jp,omitempty"`
	MalID   int    `json:"mal_id"`
	URL     string `json:"url"`
}

// RescrapeReport is everything the pass learned, for when the bare URL list is
// not enough to explain a number.
type RescrapeReport struct {
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	// Total is every anime row in postgres.
	Total int `json:"total_anime"`
	// WithImage resolved to an object under either key.
	WithImage int `json:"with_image"`
	// Missing has no object and a MyAnimeList reference to scrape from.
	Missing int `json:"missing"`
	// Unscrapeable has no object *and* no mal_id, so there is nowhere to go.
	// Reported rather than silently dropped: these need their link record
	// rebuilt before any scrape can help them.
	Unscrapeable []RescrapeTarget `json:"unscrapeable"`
	Targets      []RescrapeTarget `json:"targets"`
}

// RescrapeTargets correlates postgres against the bucket and returns the anime
// that have no image, with a MyAnimeList URL for each.
//
// Postgres rather than the MySQL side because that is where mal_id lives —
// MySQL has no column to build a URL from. It is also the scraper's own source
// of truth, so a target list drawn from it matches what a rescrape will write
// back.
//
// One bucket listing, not a HEAD per anime: at ~32,000 rows the per-object
// approach is half an hour of round trips against seconds for the listing.
//
// An anime counts as covered if an object exists under its id *or* under its
// legacy title slug. The backfill deliberately leaves shared-title records on
// the old key — it cannot tell whose object it is — and the frontend falls back
// to that key, so those still render. Rescraping them would refetch art that is
// already being served.
func RescrapeTargets(ctx context.Context, pg *db.DB, store storage.Storage) (*RescrapeReport, error) {
	log := logger.FromCtx(ctx)

	log.Info("listing bucket")
	present := map[string]struct{}{}
	for entry := range store.List(ctx, "/", false) {
		if entry.Err != nil {
			return nil, entry.Err
		}
		present[basename(entry.Path)] = struct{}{}
	}
	log.Info("bucket listed", zap.Int("objects", len(present)))

	// Postgres has no equivalent of Vitess's 100,000 row cap, so this is one
	// query rather than the paged walk the backfill needs.
	//
	// The link is preferred where it exists because it is the exact address the
	// scraper recorded; mal_id builds the canonical form otherwise, which
	// MyAnimeList redirects to the slug anyway. Every anime with a link also
	// has a mal_id, so the join only ever improves a URL, never adds one.
	rows, err := pg.DB.WithContext(ctx).Raw(`
		SELECT a.id::text, COALESCE(a.title_en, ''), COALESCE(a.title_jp, ''),
		       COALESCE(a.mal_id, 0), COALESCE(MAX(ml.link), '')
		FROM anime a
		LEFT JOIN myanimelist_link ml ON ml.anime_id = a.id::text
		GROUP BY a.id, a.title_en, a.title_jp, a.mal_id
		ORDER BY a.id
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	report := &RescrapeReport{Targets: []RescrapeTarget{}, Unscrapeable: []RescrapeTarget{}}

	for rows.Next() {
		var (
			id      string
			titleEn string
			titleJp string
			malID   int
			link    string
		)
		if err := rows.Scan(&id, &titleEn, &titleJp, &malID, &link); err != nil {
			return nil, err
		}
		report.Total++

		if hasImage(present, id, titleEn, titleJp) {
			report.WithImage++
			continue
		}

		target := RescrapeTarget{ID: id, TitleEn: titleEn, TitleJp: titleJp, MalID: malID, URL: link}
		if target.URL == "" && malID > 0 {
			target.URL = fmt.Sprintf("https://myanimelist.net/anime/%d", malID)
		}
		if target.URL == "" {
			report.Unscrapeable = append(report.Unscrapeable, target)
			continue
		}
		report.Targets = append(report.Targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	report.Missing = len(report.Targets)
	log.Info("correlation complete",
		zap.Int("total", report.Total),
		zap.Int("withImage", report.WithImage),
		zap.Int("targets", report.Missing),
		zap.Int("unscrapeable", len(report.Unscrapeable)))

	return report, nil
}

// hasImage reports whether the bucket holds an object for this anime, under
// either the id or one of the legacy title slugs.
func hasImage(present map[string]struct{}, id, titleEn, titleJp string) bool {
	if _, ok := present[url.QueryEscape(id)]; ok {
		return true
	}
	for _, t := range []string{titleEn, titleJp} {
		if t == "" {
			continue
		}
		if _, ok := present[titleSlug(t)]; ok {
			return true
		}
	}
	return false
}
