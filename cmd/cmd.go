package cmd

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/psanford/claude"
	"github.com/psanford/code-buddy/interactive"
	"github.com/spf13/cobra"
)

var (
	modelFlag string
	debugLog  string
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

		r := interactive.Runner{
			APIKey: apiKey,
			Model:  modelFlag,
		}

		if debugLog != "" {
			f, err := os.OpenFile(debugLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			r.DebugLogger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
		}

		err := r.Run(ctx)
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() error {
	rootCmd.Flags().StringVar(&modelFlag, "model", claude.Claude3Dot5Sonnet, "model")
	rootCmd.Flags().StringVar(&debugLog, "debug-log", "", "Path to write debug log")

	return rootCmd.Execute()
}
