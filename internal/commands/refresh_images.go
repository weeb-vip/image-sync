package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	epNats "github.com/ThatCatDev/ep/v2/drivers/nats"
	"github.com/spf13/cobra"
	"github.com/weeb-vip/image-sync/config"
	"github.com/weeb-vip/image-sync/internal/db"
	"github.com/weeb-vip/image-sync/internal/eventing"
	"github.com/weeb-vip/image-sync/internal/logger"
	"github.com/weeb-vip/image-sync/internal/services/image_backfill"
	"github.com/weeb-vip/image-sync/internal/services/image_processor"
	"github.com/weeb-vip/image-sync/internal/services/storage/minio"
	"go.uber.org/zap"
)

var refreshImagesCmd = &cobra.Command{
	Use:   "refresh-images",
	Short: "Re-pull the art of chosen anime from the source and announce it",
	Long: `Fetches each listed anime's art again from the image_url its record carries
-- the larger MyAnimeList copy where there is one -- writes it over the object
under the anime's id, and announces it on the image-stored subject, so the
upscaler takes it from there. The skip rule the consumer applies (same source,
same length) is deliberately not applied: this is the re-pull.

Rescraping does not do this: a rescrape that finds the same data changes no
row, so no event fires. This goes straight to the bucket for a chosen set.

  ./main refresh-images --ids-file front-page.txt --dry-run
  ./main refresh-images --ids a1,b2
  ./main refresh-images --ids-file front-page.txt --workers 4`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		workers, _ := cmd.Flags().GetInt("workers")
		idsFlag, _ := cmd.Flags().GetString("ids")
		idsFile, _ := cmd.Flags().GetString("ids-file")

		ids, err := collectIDs(idsFlag, idsFile)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("nothing to refresh: pass --ids or --ids-file")
		}

		cfg := config.LoadConfigOrPanic()
		log := logger.Get()
		ctx := logger.WithCtx(context.Background(), log)

		database := db.NewDB(cfg.DBConfig)
		store := minio.NewMinioStorage(cfg.MinioConfig)

		// The same announcement the consumer makes, so the upscaler sees a
		// refreshed object exactly as it sees a new one.
		var announce image_processor.Publish
		if !dryRun && cfg.NatsConfig.StoredSubject != "" {
			driver := epNats.NewNatsDriver(&epNats.Config{URL: cfg.NatsConfig.URL})
			defer driver.Close()
			announce = eventing.NatsProducer(ctx, driver, cfg.NatsConfig.StoredSubject)
		}
		processor := image_processor.NewImageProcessor[*epNats.Message](store, announce)

		log.Info("refreshing images",
			zap.Int("ids", len(ids)), zap.Bool("dryRun", dryRun), zap.Int("workers", workers),
			zap.String("bucket", cfg.MinioConfig.Bucket), zap.String("prefix", cfg.MinioConfig.Prefix))

		stats, err := image_backfill.New(database, store, image_backfill.Options{}).
			RefreshImages(ctx, ids, image_backfill.RefreshOptions{DryRun: dryRun, Workers: workers},
				func(ctx context.Context, id, imageURL string) error {
					return processor.Refresh(ctx, image_processor.ImageSchema{
						ID: id, URL: imageURL, Type: image_processor.DataTypeAnime,
					})
				})

		log.Info("refresh-images finished", zap.Any("stats", stats))
		return err
	},
}

// collectIDs merges --ids (comma-separated) and --ids-file (one per line,
// blank lines and # comments ignored), in order, without duplicates.
func collectIDs(idsFlag, idsFile string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") || seen[s] {
			return
		}
		seen[s] = true
		ids = append(ids, s)
	}
	for _, s := range strings.Split(idsFlag, ",") {
		add(s)
	}
	if idsFile != "" {
		f, err := os.Open(idsFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			add(sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func init() {
	rootCmd.AddCommand(refreshImagesCmd)
	refreshImagesCmd.Flags().Bool("dry-run", false, "list what would be refreshed; fetch and write nothing")
	refreshImagesCmd.Flags().Int("workers", 4, "concurrent downloads from the source")
	refreshImagesCmd.Flags().String("ids", "", "comma-separated anime ids")
	refreshImagesCmd.Flags().String("ids-file", "", "file of anime ids, one per line")
}
