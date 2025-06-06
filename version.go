package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.7.1"

var revision = "HEAD"

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the version of connperf",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("connperf %s (revision: %s)\n", version, revision)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
