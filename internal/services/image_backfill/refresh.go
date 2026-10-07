package image_backfill

import (
	"context"
	"database/sql"
	"sync"

	"github.com/weeb-vip/image-sync/internal/logger"
	"go.uber.org/zap"
)

// RefreshStats is the tally of a refresh pass.
type RefreshStats struct {
	Requested int
	NotFound  int
	NoSource  int
	Refreshed int
	Errors    int
}

// RefreshOptions tunes a refresh pass.
type RefreshOptions struct {
	DryRun  bool
	Workers int
}

// RefreshFunc stores one anime's art anew from its source; the processor's
// Refresh is the real one.
type RefreshFunc func(ctx context.Context, id, imageURL string) error

// RefreshImages re-pulls the art of the given anime from the image_url each
// record carries, whether or not the bucket already holds it. The front page
// after an upscaler change, a batch of posters MyAnimeList replaced: a chosen
// set rather than the catalogue, so this takes ids and asks nothing of the
// bucket. Each refreshed object is announced, so the upscaler takes it from
// there.
func (b *Backfiller) RefreshImages(ctx context.Context, ids []string, opts RefreshOptions, refresh RefreshFunc) (RefreshStats, error) {
	log := logger.FromCtx(ctx)
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	stats := RefreshStats{Requested: len(ids)}
	if len(ids) == 0 {
		return stats, nil
	}

	rows, err := b.DB.DB.WithContext(ctx).Table("anime").Select("id, image_url").Where("id IN ?", ids).Rows()
	if err != nil {
		return stats, err
	}
	type job struct{ id, src string }
	var jobs []job
	found := map[string]bool{}
	for rows.Next() {
		var (
			id       string
			imageURL sql.NullString
		)
		if err := rows.Scan(&id, &imageURL); err != nil {
			rows.Close()
			return stats, err
		}
		found[id] = true
		if !imageURL.Valid || imageURL.String == "" {
			stats.NoSource++
			log.Warn("no image_url on record", zap.String("id", id))
			continue
		}
		jobs = append(jobs, job{id: id, src: imageURL.String})
	}
	rows.Close()
	for _, id := range ids {
		if !found[id] {
			stats.NotFound++
			log.Warn("no such anime", zap.String("id", id))
		}
	}

	if opts.DryRun {
		for _, j := range jobs {
			log.Info("would refresh", zap.String("id", j.id), zap.String("url", j.src))
		}
		stats.Refreshed = len(jobs)
		return stats, nil
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
		ch = make(chan job)
	)
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if err := refresh(ctx, j.id, j.src); err != nil {
					log.Error("refresh failed", zap.String("id", j.id), zap.String("url", j.src), zap.Error(err))
					mu.Lock()
					stats.Errors++
					mu.Unlock()
					continue
				}
				log.Info("refreshed", zap.String("id", j.id), zap.String("url", j.src))
				mu.Lock()
				stats.Refreshed++
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		select {
		case ch <- j:
		case <-ctx.Done():
			close(ch)
			wg.Wait()
			return stats, ctx.Err()
		}
	}
	close(ch)
	wg.Wait()
	log.Info("refresh complete", zap.Any("stats", stats))
	return stats, nil
}
