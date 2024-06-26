package cmd

import (
	"github.com/psanford/code-buddy/interactive"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "code-buddy",
	Short: "A Claude Code Exploration Tool",
}

func Execute() error {
	rootCmd.AddCommand(interactive.Command())

	return rootCmd.Execute()
}
