package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/psanford/claude"
	"github.com/psanford/code-buddy/config"
	"github.com/psanford/code-buddy/interactive"
	"github.com/spf13/cobra"
)

var (
	modelFlag string
	debugLog  string
	useBase64 bool
)
var rootCmd = &cobra.Command{
	Use:   "code-buddy",
	Short: "A Claude Code Exploration Tool",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		var apiKey string

		conf, err := config.LoadConfig()
		if err != nil && err != config.NoConfigErr {
			log.Fatalf("Read config file err: %s", err)
		}

		apiKey = conf.AnthropicApiKey

		if apiKey == "" {
			apiKey = os.Getenv("CLAUDE_API_KEY")
			if apiKey == "" {
				log.Fatalf("No API key found in config file %s or environment variable CLAUDE_API_KEY", config.ConfigFilePath())
			}
		}

		r := interactive.Runner{
			APIKey:    apiKey,
			Model:     modelFlag,
			UseBase64: useBase64,
		}

		if debugLog != "" {
			f, err := os.OpenFile(debugLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			r.DebugLogger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
			r.DebugLogger.Debug("start debug logger")
		}

		err = r.Run(ctx)
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() error {
	models := claude.CurrentModels()
	rootCmd.Flags().StringVar(&modelFlag, "model", claude.Claude3Dot5Sonnet, fmt.Sprintf("model name (%s)", strings.Join(models, ",")))
	rootCmd.Flags().StringVar(&debugLog, "debug-log", "", "Path to write debug log")
	rootCmd.Flags().BoolVar(&useBase64, "b64", false, "Use base64 for function parameters")

	return rootCmd.Execute()
}
