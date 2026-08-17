package commands

import (
	"context"
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/image_backfill"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
)

var reportMissingImagesCmd = &cobra.Command{
	Use:   "report-missing-images",
	Short: "List anime with no poster in the bucket, as JSON",
	Long: `Reports every anime that has no poster object, so they can be rescraped.

Read-only: it lists the bucket once and walks the anime table against it,
writing nothing to either.

An anime counts as covered if an object exists under its id or under its legacy
title slug, because the backfill deliberately leaves some records on the old key
— where two anime share a title it cannot tell whose object it is. Those still
render, so reporting them would send you refetching art that is already there.

Output goes to stdout by default and logs go to stderr, so it can be redirected
straight into a file:

  ./main report-missing-images > missing.json
  ./main report-missing-images --out missing.json

Entries with an image_url had a source address the sync failed to copy; entries
without one have nothing to fetch and need the record itself rebuilt.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, _ := cmd.Flags().GetString("out")

		cfg := config.LoadConfigOrPanic()
		log := logger.Get()
		ctx := logger.WithCtx(context.Background(), log)

		database := db.NewDB(cfg.DBConfig)
		store := minio.NewMinioStorage(cfg.MinioConfig)

		log.Info("reporting anime with no poster",
			zap.String("bucket", cfg.MinioConfig.Bucket),
			zap.String("prefix", cfg.MinioConfig.Prefix))

		report, err := image_backfill.New(database, store, image_backfill.Options{}).MissingImages(ctx)
		if err != nil {
			return err
		}
		report.Bucket = cfg.MinioConfig.Bucket
		report.Prefix = cfg.MinioConfig.Prefix

		w := os.Stdout
		if out != "" && out != "-" {
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}

		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}

		log.Info("report written", zap.String("out", out), zap.Int("withoutImage", report.WithoutImage))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reportMissingImagesCmd)

	reportMissingImagesCmd.Flags().String("out", "-", "file to write the JSON report to, or - for stdout")
}
