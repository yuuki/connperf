/*
Copyright © 2021 Yuuki Tsubouchi <yuki.tsubo@gmail.com>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

var (
	// Mode flags
	clientMode bool
	serverMode bool

	// Client-specific flags
	protocol             string
	intervalStats        time.Duration
	connectFlavor        string
	connections          int32
	connectRate          int32
	duration             time.Duration
	messageBytes         int32
	showOnlyResults      bool
	mergeResultsEachHost bool
	jsonlines            bool
	pprofMutex           sync.RWMutex
	pprof                bool
	pprofAddr            string
	addrsFile            bool

	// Server-specific flags
	listenAddrs     []string
	listenAddrsFile string
	serveProtocol   string
)

func init() {
	// Mode flags
	pflag.BoolVarP(&clientMode, "client", "c", false, "run in client mode")
	pflag.BoolVarP(&serverMode, "server", "s", false, "run in server mode")

	// Client flags
	pflag.StringVar(&protocol, "proto", "tcp", "protocol (tcp or udp)")
	pflag.DurationVar(&intervalStats, "interval", 5*time.Second, "interval for printing stats")
	pflag.StringVar(&connectFlavor, "flavor", flavorPersistent,
		fmt.Sprintf("connect behavior type '%s' or '%s'", flavorPersistent, flavorEphemeral))
	pflag.Int32Var(&connections, "connections", 10,
		fmt.Sprintf("Number of concurrent connections to keep (only for '%s')", flavorPersistent))
	pflag.Int32Var(&connectRate, "rate", 100,
		fmt.Sprintf("New connections throughput (/s) (only for '%s')", flavorEphemeral))
	pflag.DurationVar(&duration, "duration", 10*time.Second, "measurement period")
	pflag.Int32Var(&messageBytes, "message-bytes", 64, "TCP/UDP message size (bytes)")
	pflag.BoolVar(&showOnlyResults, "show-only-results", false, "print only results of measurement stats")
	pflag.BoolVar(&mergeResultsEachHost, "merge-results-each-host", false, "merge results of each host (with --show-only-results)")
	pflag.BoolVar(&jsonlines, "jsonlines", false, "output results in JSON Lines format")
	pflag.BoolVar(&addrsFile, "addrs-file", false, "enable to pass a file including a pair of addresses and ports to an argument")
	pflag.BoolVar(&pprof, "enable-pprof", false, "enable pprof profiling")
	pflag.StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listening address:port")

	// Server flags
	pflag.StringVar(&serveProtocol, "protocol", "all", "listening protocol ('tcp' or 'udp')")
	pflag.StringVar(&listenAddrsFile, "listen-addrs-file", "", "enable to pass a file including a pair of addresses and ports")

	viper.BindPFlags(pflag.CommandLine)
}

func main() {
	pflag.Parse()

	// Handle version flag
	handleVersion()

	// Handle listen addresses for server mode
	if serverMode {
		// Default listen address if none provided
		if len(pflag.Args()) == 0 {
			listenAddrs = []string{"0.0.0.0:9100"}
		} else {
			listenAddrs = pflag.Args()
		}
	}

	// Validate mode selection
	if clientMode && serverMode {
		fmt.Fprintf(os.Stderr, "Error: cannot specify both client (-c) and server (-s) modes\n")
		os.Exit(1)
	}

	if !clientMode && !serverMode {
		fmt.Fprintf(os.Stderr, "Error: must specify either client (-c) or server (-s) mode\n")
		printUsage()
		os.Exit(1)
	}

	var err error
	if serverMode {
		err = runServer()
	} else {
		err = runClient()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] <addresses...>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "tcpulse is a concurrent TCP/UDP load generator that provides fine-grained, flow-level control\n\n")
	fmt.Fprintf(os.Stderr, "Modes:\n")
	fmt.Fprintf(os.Stderr, "  -c, --client    Run in client mode (connect to servers)\n")
	fmt.Fprintf(os.Stderr, "  -s, --server    Run in server mode (accept connections)\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s -s                          # Start server on default port 9100\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -s 0.0.0.0:8080             # Start server on port 8080\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -c localhost:9100           # Connect to server as client\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -c --connections 50 host:port # Connect with 50 connections\n", os.Args[0])
}

func runServer() error {
	if err := SetRLimitNoFile(); err != nil {
		return fmt.Errorf("setting file limit: %w", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(), unix.SIGINT, unix.SIGTERM)
	defer stop()

	if listenAddrsFile != "" {
		addrs, err := getAddrsFromFile(listenAddrsFile)
		if err != nil {
			return fmt.Errorf("reading addresses from file: %w", err)
		}
		listenAddrs = addrs
	}

	fmt.Printf("Listening at %q ...\n", listenAddrs)

	config := ServerConfig{
		ListenAddrs: listenAddrs,
		Protocol:    serveProtocol,
	}

	server := NewServer(config)
	return server.Start(ctx)
}

func runClient() error {
	args := pflag.Args()

	// Validate client arguments and flags
	switch connectFlavor {
	case flavorPersistent, flavorEphemeral:
	default:
		return fmt.Errorf("unexpected connect flavor %q", connectFlavor)
	}

	switch protocol {
	case "tcp", "udp":
	default:
		return fmt.Errorf("unexpected protocol %q", protocol)
	}

	if len(args) < 1 {
		return fmt.Errorf("required addresses")
	}

	if addrsFile && len(args) != 1 {
		return fmt.Errorf("the number of addresses file must be one")
	}

	if mergeResultsEachHost && !showOnlyResults {
		return fmt.Errorf("--merge-results-each-host flag requires --show-only-results flag")
	}

	setPprofServer()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := SetRLimitNoFile(); err != nil {
		return fmt.Errorf("setting file limit: %w", err)
	}

	addrs := args
	if addrsFile {
		var err error
		addrs, err = getAddrsFromFile(args[0])
		if err != nil {
			return err
		}
	}

	if !jsonlines {
		printStatHeader(os.Stdout)
	}

	config := ClientConfig{
		Protocol:             protocol,
		ConnectFlavor:        connectFlavor,
		Connections:          connections,
		ConnectRate:          connectRate,
		Duration:             duration,
		MessageBytes:         messageBytes,
		MergeResultsEachHost: mergeResultsEachHost,
		JSONLines:            jsonlines,
	}

	client := NewClient(config)

	eg, ctx := errgroup.WithContext(ctx)
	for _, addr := range addrs {
		addr := addr
		eg.Go(func() error {
			if showOnlyResults || jsonlines {
				return client.ConnectToAddresses(ctx, []string{addr})
			}
			runStatLinePrinter(ctx, os.Stdout, addr, intervalStats, mergeResultsEachHost)
			return client.ConnectToAddresses(ctx, []string{addr})
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	if jsonlines {
		printJSONLinesReport(os.Stdout, addrs, mergeResultsEachHost)
	} else {
		printReport(os.Stdout, addrs, mergeResultsEachHost)
	}
	return nil
}

func setPprofServer() {
	pprofMutex.RLock()
	enablePprof := pprof
	addr := pprofAddr
	pprofMutex.RUnlock()

	if !enablePprof {
		return
	}
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Error("pprof server error", "error", err)
		}
	}()
}
