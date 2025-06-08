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
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
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
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect connects to a port where 'serve' listens",
	Args: func(cmd *cobra.Command, args []string) error {
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

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		setPprofServer()
		return runConnectCmd(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)

	connectCmd.Flags().StringVarP(&protocol, "proto", "p", "tcp", "protocol (tcp or udp)")
	i := connectCmd.Flags().IntP("interval", "i", 5, "interval for printing stats")
	intervalStats = time.Duration(*i) * time.Second
	connectCmd.Flags().StringVarP(&connectFlavor, "flavor", "f", flavorPersistent,
		fmt.Sprintf("connect behavior type '%s' or '%s'", flavorPersistent, flavorEphemeral))
	connectCmd.Flags().Int32VarP(&connections, "connections", "c", 10,
		fmt.Sprintf("Number of concurrent connections to keep (only for '%s')", flavorPersistent))
	connectCmd.Flags().Int32VarP(&connectRate, "rate", "r", 100,
		fmt.Sprintf("New connections throughput (/s) (only for '%s')", flavorEphemeral))
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "measurement period")
	connectCmd.Flags().Int32Var(&messageBytes, "message-bytes", 64, "TCP/UDP message size (bytes)")
	connectCmd.Flags().BoolVar(&showOnlyResults, "show-only-results", false, "print only results of measurement stats")
	connectCmd.Flags().BoolVar(&mergeResultsEachHost, "merge-results-each-host", false, "merge results of each host (with --show-only-results)")
	connectCmd.Flags().BoolVar(&jsonlines, "jsonlines", false, "output results in JSON Lines format")
	connectCmd.Flags().BoolVar(&addrsFile, "addrs-file", false, "enable to pass a file including a pair of addresses and ports to an argument")

	connectCmd.Flags().BoolVar(&pprof, "enable-pprof", false, "enable pprof profiling")
	connectCmd.Flags().StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listening address:port")
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

func runConnectCmd(cmd *cobra.Command, args []string) error {
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
		printStatHeader(cmd.OutOrStdout())
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
			runStatLinePrinter(ctx, cmd.OutOrStdout(), addr, intervalStats, mergeResultsEachHost)
			return client.ConnectToAddresses(ctx, []string{addr})
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	if jsonlines {
		printJSONLinesReport(cmd.OutOrStdout(), addrs, mergeResultsEachHost)
	} else {
		printReport(cmd.OutOrStdout(), addrs, mergeResultsEachHost)
	}
	return nil
}
