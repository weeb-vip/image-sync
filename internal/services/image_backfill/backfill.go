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
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/storage"
	"go.uber.org/zap"
)

// Group names, in the order Run walks them. Exported so the command can
// validate --type against exactly what Run understands rather than a second,
// drifting copy of the list.
const (
	GroupAnime     = "anime"
	GroupCharacter = "character"
	GroupStaff     = "staff"
)

// Groups is every group a run can cover.
var Groups = []string{GroupAnime, GroupCharacter, GroupStaff}

// ValidateTypes reports any --type value that is not a known group. Without
// this an unrecognised name silently matched nothing and the run exited
// successfully having copied not one object.
func ValidateTypes(types []string) error {
	var unknown []string
	for _, t := range types {
		ok := false
		for _, g := range Groups {
			if strings.EqualFold(t, g) {
				ok = true
				break
			}
		}
		if !ok {
			unknown = append(unknown, t)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown --type %s (known: %s)",
			strings.Join(unknown, ", "), strings.Join(Groups, ", "))
	}
	return nil
}

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
		opts.Workers = 32
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
		{name: GroupAnime, dir: "/", recursive: false, buildIdx: b.animeIndex},
		{name: GroupCharacter, dir: "/characters/", recursive: true, buildIdx: b.characterIndex},
		{name: GroupStaff, dir: "/staff/", recursive: true, buildIdx: b.staffIndex},
	}

	// A group that fails does not take the others down with it. The groups are
	// independent — different tables, different prefixes — and aborting the run
	// on the first failure is why staff was never even attempted after the
	// character index hit PlanetScale's row cap. Failures are collected and
	// returned at the end, so the command still exits non-zero.
	var (
		total  Stats
		failed []error
	)
	for _, g := range all {
		if !b.wants(g.name) {
			continue
		}

		if ctx.Err() != nil {
			return total, ctx.Err()
		}

		log.Info("building index", zap.String("group", g.name))
		idx, err := g.buildIdx(ctx)
		if err != nil {
			log.Error("index failed, skipping group",
				zap.String("group", g.name), zap.Error(err))
			failed = append(failed, fmt.Errorf("%s index: %w", g.name, err))
			continue
		}
		log.Info("index built",
			zap.String("group", g.name),
			zap.Int("keys", len(idx.byKey)),
			zap.Int("ambiguousKeys", len(idx.ambiguous)),
			zap.Int("ids", len(idx.ids)))

		stats, err := b.runGroup(ctx, g, idx)
		total.add(stats)
		log.Info("group complete", zap.String("group", g.name), zap.Any("stats", stats))
		if err != nil {
			log.Error("group failed", zap.String("group", g.name), zap.Error(err))
			failed = append(failed, fmt.Errorf("%s: %w", g.name, err))
		}
	}

	return total, errors.Join(failed...)
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

	// stats is the walker's own tally, touched only here; the workers keep a
	// separate one behind mu and the two are merged once they are done.
	var (
		mu     sync.Mutex
		stats  Stats
		work   Stats
		wg     sync.WaitGroup
		listed = b.Storage.List(ctx, g.dir, g.recursive)
	)

	// The existence check belongs here, not on the walker. It is a HEAD round
	// trip per object, and running it inline capped the whole pass at one
	// request at a time — a few dozen objects a second no matter how many
	// workers were configured.
	jobs := make(chan [2]string)
	for i := 0; i < b.Options.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				src, dst := job[0], job[1]

				if !b.Options.Overwrite {
					exists, err := b.Storage.Exists(ctx, dst)
					if err != nil {
						log.Error("stat failed", zap.String("path", dst), zap.Error(err))
						mu.Lock()
						work.Errors++
						mu.Unlock()
						continue
					}
					if exists {
						mu.Lock()
						work.AlreadyDone++
						mu.Unlock()
						continue
					}
				}

				if b.Options.DryRun {
					log.Info("would copy", zap.String("src", src), zap.String("dst", dst))
					mu.Lock()
					work.Copied++
					mu.Unlock()
					continue
				}

				err := b.Storage.Copy(ctx, src, dst)
				mu.Lock()
				if err != nil {
					work.Errors++
				} else {
					work.Copied++
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

		key := basename(entry.Path)
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

	stats.add(work)
	return stats, listErr
}

// basename is the object key without its prefix directory.
func basename(p string) string {
	return path.Base(p)
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

// indexBatchSize keeps each index query under Vitess's 100,000 row-per-query
// cap. PlanetScale aborts anything larger outright — "Row count exceeded
// 100000" — which is what stopped the character index dead once anime_character
// grew past it.
const indexBatchSize = 20000

// scanAll walks a table in id order, a batch at a time, calling fn for each row.
// fn returns the row's id, which becomes the cursor for the next batch — keyset
// paging rather than OFFSET, so it stays cheap however deep it goes.
func (b *Backfiller) scanAll(ctx context.Context, table, columns string, fn func(*sql.Rows) (string, error)) error {
	last := ""
	for {
		rows, err := b.DB.DB.WithContext(ctx).
			Table(table).
			Select(columns).
			Where("id > ?", last).
			Order("id").
			Limit(indexBatchSize).
			Rows()
		if err != nil {
			return err
		}

		n := 0
		for rows.Next() {
			id, err := fn(rows)
			if err != nil {
				rows.Close()
				return err
			}
			last = id
			n++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		// A short batch means the table is exhausted.
		if n < indexBatchSize {
			return nil
		}
	}
}

func (b *Backfiller) animeIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	err := b.scanAll(ctx, "anime", "id, title_en, title_jp", func(rows *sql.Rows) (string, error) {
		var (
			id      string
			titleEn *string
			titleJp *string
		)
		if err := rows.Scan(&id, &titleEn, &titleJp); err != nil {
			return "", err
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
		return id, nil
	})

	return idx, err
}

func (b *Backfiller) characterIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	err := b.scanAll(ctx, "anime_character", "id, anime_id, name", func(rows *sql.Rows) (string, error) {
		var id, animeID, name string
		if err := rows.Scan(&id, &animeID, &name); err != nil {
			return "", err
		}
		idx.putID(id)
		// Two producers wrote characters over time: the pulsar path keyed them
		// "<name>_<anime id>", the kafka path used the bare name. Both shapes
		// are in the bucket.
		idx.put(url.QueryEscape(name+"_"+animeID), id)
		idx.put(url.QueryEscape(name), id)
		return id, nil
	})

	return idx, err
}

func (b *Backfiller) staffIndex(ctx context.Context) (*index, error) {
	idx := newIndex()

	err := b.scanAll(ctx, "anime_staff", "id, given_name, family_name", func(rows *sql.Rows) (string, error) {
		var id, given, family string
		if err := rows.Scan(&id, &given, &family); err != nil {
			return "", err
		}
		idx.putID(id)
		idx.put(url.QueryEscape(given+"_"+family), id)
		return id, nil
	})

	return idx, err
}
