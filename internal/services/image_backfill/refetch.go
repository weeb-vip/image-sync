package image_backfill

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/weeb-vip/image-sync/internal/logger"
	"go.uber.org/zap"
)

// RefetchStats is the tally of a refetch pass.
type RefetchStats struct {
	// Anime is every row considered.
	Anime int
	// AlreadyPresent already has an object under its id; nothing to do.
	AlreadyPresent int
	// Fetched was downloaded from image_url and stored under the id.
	Fetched int
	// NoSource has no object and no image_url, so there is nothing to fetch.
	// These need their record rebuilt before anything here can help.
	NoSource int
	Errors   int
}

// RefetchOptions tunes a refetch pass.
type RefetchOptions struct {
	DryRun bool
	// Workers bounds concurrent downloads. Lower than the backfill's default
	// on purpose: these are fetches against MyAnimeList's CDN, not
	// server-side copies within our own bucket.
	Workers int
	// Limit stops after this many fetches. Zero means no limit. Useful for a
	// cautious first run against a live third party.
	Limit int
}

// SyncMissingImages fetches a poster for every anime that has no object under
// its id, using the image_url the record already carries.
//
// This exists because neither of the obvious approaches works for these rows.
// A rescrape rewrites identical data, so no row changes, so no CDC event fires
// and no image message is ever published — measured: of anime re-scraped with
// unchanged data, none gained an image, while every row that genuinely changed
// did. And the backfill cannot copy them, because where two anime share a title
// they shared one object and it cannot tell whose art it holds.
//
// Every anime row already knows where its own art lives. Fetching straight from
// that is precise per id, needs no scrape, no message, and no deduplication
// first — and it is idempotent, so it can be re-run until the count is zero.
func (b *Backfiller) SyncMissingImages(ctx context.Context, opts RefetchOptions) (RefetchStats, error) {
	log := logger.FromCtx(ctx)

	if opts.Workers <= 0 {
		opts.Workers = 8
	}

	log.Info("listing bucket")
	present := map[string]struct{}{}
	for entry := range b.Storage.List(ctx, "/", false) {
		if entry.Err != nil {
			return RefetchStats{}, entry.Err
		}
		present[basename(entry.Path)] = struct{}{}
	}
	log.Info("bucket listed", zap.Int("objects", len(present)))

	type job struct{ id, src string }

	var (
		mu    sync.Mutex
		work  RefetchStats
		wg    sync.WaitGroup
		jobs  = make(chan job)
		stats RefetchStats
	)

	client := &http.Client{Timeout: 30 * time.Second}

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				data, err := fetch(ctx, client, j.src)
				if err != nil {
					log.Error("fetch failed",
						zap.String("id", j.id), zap.String("url", j.src), zap.Error(err))
					mu.Lock()
					work.Errors++
					mu.Unlock()
					continue
				}

				path := "/" + url.QueryEscape(j.id)
				if err := b.Storage.Put(ctx, data, path); err != nil {
					log.Error("store failed", zap.String("path", path), zap.Error(err))
					mu.Lock()
					work.Errors++
					mu.Unlock()
					continue
				}

				log.Info("fetched",
					zap.String("id", j.id), zap.String("url", j.src), zap.Int("bytes", len(data)))
				mu.Lock()
				work.Fetched++
				mu.Unlock()
			}
		}()
	}

	queued := 0
	scanErr := b.scanAll(ctx, "anime", "id, image_url", func(rows *sql.Rows) (string, error) {
		var (
			id       string
			imageURL *string
		)
		if err := rows.Scan(&id, &imageURL); err != nil {
			return "", err
		}
		stats.Anime++

		if _, ok := present[url.QueryEscape(id)]; ok {
			stats.AlreadyPresent++
			return id, nil
		}
		if imageURL == nil || *imageURL == "" {
			stats.NoSource++
			return id, nil
		}
		if opts.Limit > 0 && queued >= opts.Limit {
			return id, nil
		}

		if opts.DryRun {
			log.Info("would fetch", zap.String("id", id), zap.String("url", *imageURL))
			stats.Fetched++
			queued++
			return id, nil
		}

		select {
		case jobs <- job{id: id, src: *imageURL}:
			queued++
		case <-ctx.Done():
			return id, ctx.Err()
		}
		return id, nil
	})

	close(jobs)
	wg.Wait()

	stats.Fetched += work.Fetched
	stats.Errors += work.Errors

	log.Info("refetch complete", zap.Any("stats", stats))
	return stats, scanErr
}

func fetch(ctx context.Context, client *http.Client, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	// MyAnimeList's CDN serves a challenge page to unrecognised clients, and a
	// challenge stored as a poster is worse than no poster at all.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; weeb-vip-image-sync/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	return data, nil
}
