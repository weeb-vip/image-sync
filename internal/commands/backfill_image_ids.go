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

var backfillImageIDsCmd = &cobra.Command{
	Use:   "backfill-image-ids",
	Short: "Copy every name-keyed image in the bucket to its id-keyed path",
	Long: `Walks the bucket once and, for every image still stored under a name-derived
slug, copies it to a path keyed by the record id instead.

The name -> id mapping comes from the database: each row's slug is recomputed
exactly as the producers computed it, and the bucket listing is matched against
that. Objects the index cannot explain, and slugs that two rows both claim, are
reported and left alone rather than guessed at.

Originals are left in place. Re-running is safe: objects already at their id
path are skipped unless --overwrite is passed.

Banners are already keyed by anime id and are never touched.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		overwrite, _ := cmd.Flags().GetBool("overwrite")
		types, _ := cmd.Flags().GetStringSlice("type")
		workers, _ := cmd.Flags().GetInt("workers")

		cfg := config.LoadConfigOrPanic()
		log := logger.Get()
		ctx := logger.WithCtx(context.Background(), log)

		database := db.NewDB(cfg.DBConfig)
		store := minio.NewMinioStorage(cfg.MinioConfig)

		log.Info("starting image id backfill",
			zap.Bool("dryRun", dryRun),
			zap.Bool("overwrite", overwrite),
			zap.Strings("types", types),
			zap.String("bucket", cfg.MinioConfig.Bucket),
			zap.String("prefix", cfg.MinioConfig.Prefix))

		stats, err := image_backfill.New(database, store, image_backfill.Options{
			DryRun:    dryRun,
			Overwrite: overwrite,
			Types:     types,
			Workers:   workers,
		}).Run(ctx)

		log.Info("image id backfill finished", zap.Any("stats", stats))
		return err
	},
}

func init() {
	rootCmd.AddCommand(backfillImageIDsCmd)

	backfillImageIDsCmd.Flags().Bool("dry-run", false, "report what would be copied without writing anything")
	backfillImageIDsCmd.Flags().Bool("overwrite", false, "copy even when the id-keyed object already exists")
	backfillImageIDsCmd.Flags().StringSlice("type", nil, "limit to some of anime,character,staff (default: all)")
	// Each worker does a HEAD then a server-side copy, both network-bound, so
	// this can sit well above the core count.
	backfillImageIDsCmd.Flags().Int("workers", 32, "concurrent copies")
}
