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

var reportRescrapeTargetsCmd = &cobra.Command{
	Use:   "report-rescrape-targets",
	Short: "MyAnimeList URLs for every anime with no image in the bucket",
	Long: `Correlates the postgres anime table against the bucket and writes the
MyAnimeList URL of every anime that has no image, ready to feed a scraper.

Postgres rather than the MySQL side because that is where mal_id lives — MySQL
has no column to build a URL from — and because it is the scraper's own source
of truth, so the list matches what a rescrape writes back.

Read-only. It lists the bucket once and runs one query, writing to neither.

An anime counts as covered if an object exists under its id or under its legacy
title slug, since the backfill deliberately leaves shared-title records on the
old key and the frontend falls back to it. Those still render, so rescraping
them would refetch art already being served.

  ./main report-rescrape-targets > targets.json
  ./main report-rescrape-targets --format report --out report.json

--format urls (the default) writes a plain array of URLs. --format report writes
the full picture: per-anime detail, the counts, and the anime with no
MyAnimeList reference at all, which no scrape can help until their link record
is rebuilt.

Logs go to stderr, so stdout redirects cleanly into a file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, _ := cmd.Flags().GetString("out")
		format, _ := cmd.Flags().GetString("format")

		if format != "urls" && format != "report" {
			return errUnknownFormat(format)
		}

		cfg := config.LoadConfigOrPanic()
		log := logger.Get()
		ctx := logger.WithCtx(context.Background(), log)

		pg := db.NewPostgresDB(cfg.PostgresConfig)
		store := minio.NewMinioStorage(cfg.MinioConfig)

		log.Info("correlating postgres against the bucket",
			zap.String("pgHost", cfg.PostgresConfig.Host),
			zap.String("pgDatabase", cfg.PostgresConfig.DataBase),
			zap.String("bucket", cfg.MinioConfig.Bucket),
			zap.String("prefix", cfg.MinioConfig.Prefix))

		report, err := image_backfill.RescrapeTargets(ctx, pg, store)
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

		if format == "urls" {
			urls := make([]string, 0, len(report.Targets))
			for _, t := range report.Targets {
				urls = append(urls, t.URL)
			}
			if err := enc.Encode(urls); err != nil {
				return err
			}
		} else if err := enc.Encode(report); err != nil {
			return err
		}

		log.Info("report written",
			zap.String("out", out),
			zap.String("format", format),
			zap.Int("targets", report.Missing),
			zap.Int("unscrapeable", len(report.Unscrapeable)))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reportRescrapeTargetsCmd)

	reportRescrapeTargetsCmd.Flags().String("out", "-", "file to write to, or - for stdout")
	reportRescrapeTargetsCmd.Flags().String("format", "urls", "urls (a plain array, for a scraper) or report (full detail and counts)")
}

type formatError string

func (e formatError) Error() string {
	return "unknown --format " + string(e) + " (known: urls, report)"
}

func errUnknownFormat(f string) error { return formatError(f) }
