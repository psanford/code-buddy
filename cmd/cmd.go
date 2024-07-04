package cmd

import (
	"context"
	"log"
	"os"

	"github.com/psanford/claude"
	"github.com/psanford/code-buddy/interactive"
	"github.com/spf13/cobra"
)

var (
	modelFlag string
)
var rootCmd = &cobra.Command{
	Use:   "code-buddy",
	Short: "A Claude Code Exploration Tool",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		apiKey := os.Getenv("CLAUDE_API_KEY")
		if apiKey == "" {
			log.Fatalf("Must set environment variable CLAUDE_API_KEY")
		}

		err := interactive.Run(ctx, apiKey, modelFlag)
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() error {
	rootCmd.Flags().StringVar(&modelFlag, "model", claude.Claude3Dot5Sonnet, "model")

	return rootCmd.Execute()
}
