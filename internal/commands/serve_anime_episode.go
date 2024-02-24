/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package commands

import (
	"github.com/spf13/cobra"
	"github.com/weeb-vip/image-sync/internal/eventing"
	"log"
)

// serveCmd represents the serve command
var serveAnimeEpisodeCmd = &cobra.Command{
	Use:   "serve-anime-episode",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Println("Running anime episode eventing...")
		return eventing.EventingAnimeEpisode()
	},
}

func init() {
	rootCmd.AddCommand(serveAnimeEpisodeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
