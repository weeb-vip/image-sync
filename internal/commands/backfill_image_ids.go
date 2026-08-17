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

Banners are already keyed by anime id and are never touched.

Each group is independent — its own table, its own bucket prefix — so they can
be run in isolation and in any order:

  ./main backfill-image-ids --type anime
  ./main backfill-image-ids --type character
  ./main backfill-image-ids --type staff

A group that fails no longer aborts the ones after it; the run continues and
exits non-zero at the end reporting which groups failed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		overwrite, _ := cmd.Flags().GetBool("overwrite")
		types, _ := cmd.Flags().GetStringSlice("type")
		workers, _ := cmd.Flags().GetInt("workers")

		// Fail before touching the database or the bucket: a misspelt --type
		// used to match no group at all and exit 0, reporting a clean run that
		// had copied nothing.
		if err := image_backfill.ValidateTypes(types); err != nil {
			return err
		}

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
	backfillImageIDsCmd.Flags().StringSlice("type", nil,
		"limit the run to some of anime,character,staff — repeat or comma-separate; default is all three")
	// Each worker does a HEAD then a server-side copy, both network-bound, so
	// this can sit well above the core count.
	backfillImageIDsCmd.Flags().Int("workers", 32, "concurrent copies")
}
