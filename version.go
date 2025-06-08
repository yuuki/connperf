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
	Short: "Show the version of tcpulse",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("tcpulse %s (revision: %s)\n", version, revision)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
