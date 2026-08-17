package commands

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/image_backfill"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
)

var syncMissingImagesCmd = &cobra.Command{
	Use:   "sync-missing-images",
	Short: "Fetch a poster for every anime with no object under its id",
	Long: `Fetches a poster for every anime that has no object under its id, using the
image_url the record already carries.

This is the repair for anime the backfill could not convert. Where two anime
share a title they shared one object, so the backfill cannot tell whose art it
holds and deliberately leaves it alone. Rescraping does not help either: it
rewrites identical data, so no row changes, so no CDC event fires and no image
message is ever published.

Every anime row already knows where its own art lives, so this fetches straight
from that — precise per id, no scrape, no message, no deduplication first.

Idempotent: anime that already have an object under their id are skipped, so it
can be re-run until nothing is left.

  ./main sync-missing-images --dry-run
  ./main sync-missing-images --limit 50      # cautious first run
  ./main sync-missing-images

Anime with neither an object nor an image_url are counted as NoSource: there is
nothing to fetch, and their record needs rebuilding before this can help.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		workers, _ := cmd.Flags().GetInt("workers")
		limit, _ := cmd.Flags().GetInt("limit")

		cfg := config.LoadConfigOrPanic()
		log := logger.Get()
		ctx := logger.WithCtx(context.Background(), log)

		database := db.NewDB(cfg.DBConfig)
		store := minio.NewMinioStorage(cfg.MinioConfig)

		log.Info("syncing missing images",
			zap.Bool("dryRun", dryRun),
			zap.Int("workers", workers),
			zap.Int("limit", limit),
			zap.String("bucket", cfg.MinioConfig.Bucket),
			zap.String("prefix", cfg.MinioConfig.Prefix))

		stats, err := image_backfill.New(database, store, image_backfill.Options{}).
			SyncMissingImages(ctx, image_backfill.RefetchOptions{
				DryRun:  dryRun,
				Workers: workers,
				Limit:   limit,
			})

		log.Info("sync-missing-images finished", zap.Any("stats", stats))
		return err
	},
}

func init() {
	rootCmd.AddCommand(syncMissingImagesCmd)

	syncMissingImagesCmd.Flags().Bool("dry-run", false, "report what would be fetched without downloading or writing")
	// Lower than the backfill's default on purpose: these are fetches against
	// MyAnimeList's CDN, not server-side copies inside our own bucket.
	syncMissingImagesCmd.Flags().Int("workers", 8, "concurrent downloads")
	syncMissingImagesCmd.Flags().Int("limit", 0, "stop after this many fetches (0 = no limit)")
}
