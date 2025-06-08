package main

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

const version = "0.7.2"

var revision = "HEAD"
var showVersion bool

func init() {
	pflag.BoolVar(&showVersion, "version", false, "show version information")
}

func handleVersion() {
	if showVersion {
		fmt.Printf("tcpulse %s (revision: %s)\n", version, revision)
		os.Exit(0)
	}
}
