package commands

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/weeb-vip/image-sync/internal/eventing"
)

// serveImageSyncNatsCmd is the NATS counterpart of serve-image-sync-kafka.
//
// A separate command rather than a flag: prod and staging run the same image,
// so the command name is what selects the transport, keeping that choice in the
// deployment values rather than an environment variable.
var serveImageSyncNatsCmd = &cobra.Command{
	Use:   "serve-image-sync-nats",
	Short: "Consume image sync events from NATS JetStream",
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Println("Running image sync eventing over NATS...")

		return eventing.EventingImageNats()
	},
}

func init() {
	rootCmd.AddCommand(serveImageSyncNatsCmd)
}
