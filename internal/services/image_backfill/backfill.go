// Package image_backfill re-keys the existing bucket contents from names to
// ids, in one pass.
//
// Images used to be stored under a slug derived from the record's name — the
// lowercased, underscored anime title, the character name, the staff member's
// given_family name. That is ambiguous (two shows share a title, two characters
// share a name) and it breaks the moment a title is corrected upstream, which
// silently orphans the image. Everything is keyed by the record id now.
//
// The mapping only runs one way: name -> slug is lossy, so it cannot be
// inverted from the bucket alone. The index is therefore built from the
// database — every row's slug is computed exactly as the producers computed it
// — and the bucket listing is matched against that. Anything the listing turns
// up that the index does not explain is reported, never guessed at.
//
// Objects are copied, not moved. The name-keyed original stays put so the
// frontend can be rolled back without re-running anything; pruning them is a
// separate, later decision.
package image_backfill

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
)

type Options struct {
	// DryRun reports what would be copied without writing anything.
	DryRun bool
	// Overwrite copies even when the id-keyed object already exists. Off by
	// default so a re-run is cheap and idempotent.
	Overwrite bool
	// Types limits the pass to some of "anime", "character", "staff". Empty
	// means all three. Banners are already id-keyed and are never touched.
	Types []string
	// Workers bounds concurrent copies.
	Workers int
}

type Stats struct {
	Listed      int
	Copied      int
	AlreadyDone int
	AlreadyID   int
	Unmatched   int
	Ambiguous   int
	Errors      int
}

func (s *Stats) add(other Stats) {
	s.Listed += other.Listed
	s.Copied += other.Copied
	s.AlreadyDone += other.AlreadyDone
	s.AlreadyID += other.AlreadyID
	s.Unmatched += other.Unmatched
	s.Ambiguous += other.Ambiguous
	s.Errors += other.Errors
}

type Backfiller struct {
	DB      *db.DB
	Storage storage.Storage
	Options Options
}

func New(database *db.DB, store storage.Storage, opts Options) *Backfiller {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	return &Backfiller{DB: database, Storage: store, Options: opts}
}

// group is one prefix of the bucket, paired with the index that explains it.
type group struct {
	name string
	// dir is the bucket path the objects live under, "/" for anime posters.
	dir string
	// recursive is false for the bucket root, where the other groups' own
	// directories sit alongside the anime posters.
	recursive bool
	buildIdx  func(context.Context) (*index, error)
}

func (b *Backfiller) Run(ctx context.Context) (Stats, error) {
	log := logger.FromCtx(ctx)

	all := []group{
		{name: "anime", dir: "/", recursive: false, buildIdx: b.animeIndex},
		{name: "character", dir: "/characters/", recursive: true, buildIdx: b.characterIndex},
		{name: "staff", dir: "/staff/", recursive: true, buildIdx: b.staffIndex},
	}

	var total Stats
	for _, g := range all {
		if !b.wants(g.name) {
			continue
		}
		log.Info("building index", zap.String("group", g.name))
		idx, err := g.buildIdx(ctx)
		if err != nil {
			return total, err
		}
		log.Info("index built",
			zap.String("group", g.name),
			zap.Int("keys", len(idx.byKey)),
			zap.Int("ambiguousKeys", len(idx.ambiguous)),
			zap.Int("ids", len(idx.ids)))

		stats, err := b.runGroup(ctx, g, idx)
		total.add(stats)
		if err != nil {
			return total, err
		}
		log.Info("group complete", zap.String("group", g.name), zap.Any("stats", stats))
	}

	return total, nil
}

func (b *Backfiller) wants(name string) bool {
	if len(b.Options.Types) == 0 {
		return true
	}
	for _, t := range b.Options.Types {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}

func (b *Backfiller) runGroup(ctx context.Context, g group, idx *index) (Stats, error) {
	log := logger.FromCtx(ctx)

	// stats is the walker's own tally, touched only here; the copy workers
	// keep a separate one behind mu and the two are merged once they are done.
	var (
		mu     sync.Mutex
		stats  Stats
		copied Stats
		wg     sync.WaitGroup
		listed = b.Storage.List(ctx, g.dir, g.recursive)
	)

	jobs := make(chan [2]string)
	for i := 0; i < b.Options.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				src, dst := job[0], job[1]
				err := b.Storage.Copy(ctx, src, dst)
				mu.Lock()
				if err != nil {
					copied.Errors++
				} else {
					copied.Copied++
				}
				mu.Unlock()
				if err != nil {
					log.Error("copy failed",
						zap.String("src", src), zap.String("dst", dst), zap.Error(err))
					continue
				}
				log.Info("copied", zap.String("src", src), zap.String("dst", dst))
			}
		}()
	}

	var listErr error
	for entry := range listed {
		if entry.Err != nil {
			listErr = entry.Err
			break
		}
		stats.Listed++

		key := path.Base(entry.Path)
		if _, ok := idx.ids[key]; ok {
			// Already re-keyed by an earlier run, or written by a producer
			// that is already sending ids.
			stats.AlreadyID++
			continue
		}
		if _, ok := idx.ambiguous[key]; ok {
			// Several rows produce this exact key, so the bucket cannot say
			// which one the image belongs to. Guessing would attach the wrong
			// portrait to a character; leave it and report it.
			log.Warn("ambiguous key, skipping", zap.String("group", g.name), zap.String("path", entry.Path))
			stats.Ambiguous++
			continue
		}
		id, ok := idx.byKey[key]
		if !ok {
			log.Warn("no row matches key", zap.String("group", g.name), zap.String("path", entry.Path))
			stats.Unmatched++
			continue
		}

		dst := g.dir + url.QueryEscape(id)
		if !b.Options.Overwrite {
			exists, err := b.Storage.Exists(ctx, dst)
			if err != nil {
				log.Error("stat failed", zap.String("path", dst), zap.Error(err))
				stats.Errors++
				continue
			}
			if exists {
				stats.AlreadyDone++
				continue
			}
		}

		if b.Options.DryRun {
			log.Info("would copy", zap.String("src", entry.Path), zap.String("dst", dst))
			stats.Copied++
			continue
		}

		select {
		case jobs <- [2]string{entry.Path, dst}:
		case <-ctx.Done():
			listErr = ctx.Err()
		}
		if listErr != nil {
			break
		}
	}

	close(jobs)
	wg.Wait()

	stats.add(copied)
	return stats, listErr
}

// index maps an object key back to the row that produced it.
type index struct {
	byKey     map[string]string
	ambiguous map[string]struct{}
	ids       map[string]struct{}
}

func newIndex() *index {
	return &index{
		byKey:     map[string]string{},
		ambiguous: map[string]struct{}{},
		ids:       map[string]struct{}{},
	}
}

// put registers one key a row could have been stored under. A key claimed by
// two different ids is demoted to ambiguous and never used.
func (i *index) put(key, id string) {
	if key == "" || id == "" {
		return
	}
	if _, bad := i.ambiguous[key]; bad {
		return
	}
	if existing, ok := i.byKey[key]; ok && existing != id {
		delete(i.byKey, key)
		i.ambiguous[key] = struct{}{}
		return
	}
	i.byKey[key] = id
}

func (i *index) putID(id string) {
	if id != "" {
		i.ids[url.QueryEscape(id)] = struct{}{}
	}
}

// titleSlug reproduces what anime-sync sent as the image name: the title,
// lowercased, spaces underscored, then query-escaped by image-sync on the way
// into the bucket.
func titleSlug(title string) string {
	return url.QueryEscape(strings.ReplaceAll(strings.ToLower(title), " ", "_"))
}

func (b *Backfiller) animeIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	rows, err := b.DB.DB.WithContext(ctx).
		Table("anime").
		Select("id, title_en, title_jp").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id      string
			titleEn *string
			titleJp *string
		)
		if err := rows.Scan(&id, &titleEn, &titleJp); err != nil {
			return nil, err
		}
		idx.putID(id)
		// Producers prefer title_en and fall back to title_jp, so an object
		// could be under either — index both rather than replaying that
		// preference and missing rows whose english title arrived late.
		if titleEn != nil {
			idx.put(titleSlug(*titleEn), id)
		}
		if titleJp != nil {
			idx.put(titleSlug(*titleJp), id)
		}
	}

	return idx, rows.Err()
}

func (b *Backfiller) characterIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	rows, err := b.DB.DB.WithContext(ctx).
		Table("anime_character").
		Select("id, anime_id, name").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, animeID, name string
		if err := rows.Scan(&id, &animeID, &name); err != nil {
			return nil, err
		}
		idx.putID(id)
		// Two producers wrote characters over time: the pulsar path keyed them
		// "<name>_<anime id>", the kafka path used the bare name. Both shapes
		// are in the bucket.
		idx.put(url.QueryEscape(name+"_"+animeID), id)
		idx.put(url.QueryEscape(name), id)
	}

	return idx, rows.Err()
}

func (b *Backfiller) staffIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	rows, err := b.DB.DB.WithContext(ctx).
		Table("anime_staff").
		Select("id, given_name, family_name").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, given, family string
		if err := rows.Scan(&id, &given, &family); err != nil {
			return nil, err
		}
		idx.putID(id)
		idx.put(url.QueryEscape(given+"_"+family), id)
	}

	return idx, rows.Err()
}
