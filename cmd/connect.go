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
package cmd

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/spf13/cobra"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"

	"github.com/yuuki/connperf/limit"
	"github.com/yuuki/connperf/sock"
)

const (
	flavorPersistent = "persistent"
	flavorEphemeral  = "ephemeral"
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
	connectCmd.Flags().BoolVar(&addrsFile, "addrs-file", false, "enable to pass a file including a pair of addresses and ports to an argument")

	connectCmd.Flags().BoolVar(&pprof, "enable-pprof", false, "enable pprof profiling")
	connectCmd.Flags().StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listening address:port")
}

func setPprofServer() {
	if !pprof {
		return
	}
	go func() {
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			slog.Error("pprof server error", "error", err)
		}
	}()
}

func getAddrsFromFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading addresses file: %w", err)
	}
	return strings.Fields(string(data)), nil
}

func waitLim(ctx context.Context, rl ratelimit.Limiter) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		done := make(chan struct{})
		go func() {
			rl.Take()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func getOrRegisterTimer(key, addr string) metrics.Timer {
	if mergeResultsEachHost {
		return metrics.GetOrRegisterTimer(key, nil)
	}
	return metrics.GetOrRegisterTimer(key+"."+addr, nil)
}

func unregisterTimer(key, addr string) {
	if mergeResultsEachHost {
		metrics.Unregister(key)
		return
	}
	metrics.Unregister(key + "." + addr)
}

func printReport(w io.Writer, addrs []string) {
	fmt.Fprintln(w, "--- A result during total execution time ---")
	if mergeResultsEachHost {
		ts := getOrRegisterTimer("total.latency", "")
		printStatLine(w, fmt.Sprintf("merged(%d hosts)", len(addrs)), ts)
		return
	}
	for _, addr := range addrs {
		ts := getOrRegisterTimer("total.latency", addr)
		printStatLine(w, addr, ts)
	}
}

func runConnectCmd(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := limit.SetRLimitNoFile(); err != nil {
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

	printStatHeader(cmd.OutOrStdout())

	eg, ctx := errgroup.WithContext(ctx)
	for _, addr := range addrs {
		addr := addr // Create new variable for closure
		eg.Go(func() error {
			if showOnlyResults {
				return connectAddr(ctx, addr)
			}
			runStatLinePrinter(ctx, cmd.OutOrStdout(), addr)
			return connectAddr(ctx, addr)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	printReport(cmd.OutOrStdout(), addrs)
	return nil
}

func connectAddr(ctx context.Context, addr string) error {
	switch protocol {
	case "tcp":
		switch connectFlavor {
		case flavorPersistent:
			return connectPersistent(ctx, addr)
		case flavorEphemeral:
			return connectEphemeral(ctx, addr)
		}
	case "udp":
		return connectUDP(ctx, addr)
	}
	return fmt.Errorf("invalid protocol or flavor combination")
}

func toMicroseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}

func toMicrosecondsf(n float64) int64 {
	return time.Duration(n).Microseconds()
}

func printStatHeader(w io.Writer) {
	fmt.Fprintf(w, "%-20s %-10s %-15s %-15s %-15s %-15s %-15s %-15s %-10s\n",
		"PEER", "CNT", "LAT_MAX(µs)", "LAT_MIN(µs)", "LAT_MEAN(µs)",
		"LAT_90p(µs)", "LAT_95p(µs)", "LAT_99p(µs)", "RATE(/s)")
}

func printStatLine(w io.Writer, addr string, stat metrics.Timer) {
	fmt.Fprintf(w, "%-20s %-10d %-15d %-15d %-15d %-15d %-15d %-15d %-10.2f\n",
		addr,
		stat.Count(),
		toMicroseconds(stat.Max()),
		toMicroseconds(stat.Min()),
		toMicrosecondsf(stat.Mean()),
		toMicrosecondsf(stat.Percentile(0.9)),
		toMicrosecondsf(stat.Percentile(0.95)),
		toMicrosecondsf(stat.Percentile(0.99)),
		stat.RateMean(),
	)
}

func runStatLinePrinter(ctx context.Context, w io.Writer, addr string) {
	go func() {
		ticker := time.NewTicker(intervalStats)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts := getOrRegisterTimer("tick.latency", addr)
				printStatLine(w, addr, ts)
				unregisterTimer("tick.latency", addr)
			}
		}
	}()
}

func measureTime(addr string, f func() error) error {
	ts := getOrRegisterTimer("total.latency", addr)
	is := getOrRegisterTimer("tick.latency", addr)
	start := time.Now()
	if err := f(); err != nil {
		return err
	}
	elapsed := time.Since(start)
	ts.Update(elapsed)
	is.Update(elapsed)
	return nil
}

func connectPersistent(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	bufTCPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	dialer := net.Dialer{
		Control: sock.GetTCPControlWithFastOpen(),
	}

	eg, ctx := errgroup.WithContext(ctx)
	for i := 0; i < int(connections); i++ {
		eg.Go(func() error {
			conn, err := dialer.Dial("tcp", addrport)
			if err != nil {
				return fmt.Errorf("dialing %q: %w", addrport, err)
			}
			defer conn.Close()

			msgsTotal := int64(connectRate) * int64(duration.Seconds())
			limiter := ratelimit.New(int(connectRate))

			for j := int64(0); j < msgsTotal; j++ {
				if err := waitLim(ctx, limiter); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil
					}
					continue
				}

				if err := measureTime(addrport, func() error {
					msg := bufTCPPool.Get().([]byte)
					defer bufTCPPool.Put(msg)

					if n, err := rand.Read(msg); err != nil {
						return fmt.Errorf("generating random data (length:%d): %w", n, err)
					}

					if _, err := conn.Write(msg); err != nil {
						return fmt.Errorf("writing to connection: %w", err)
					}
					if _, err := conn.Read(msg); err != nil {
						return fmt.Errorf("reading from connection: %w", err)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return eg.Wait()
}

func connectEphemeral(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	bufTCPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	dialer := net.Dialer{
		Control: sock.GetTCPControlWithFastOpen(),
	}

	connTotal := int64(connectRate) * int64(duration.Seconds())
	limiter := ratelimit.New(int(connectRate))

	eg, ctx := errgroup.WithContext(ctx)
	for i := int64(0); i < connTotal; i++ {
		if err := waitLim(ctx, limiter); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			continue
		}

		eg.Go(func() error {
			return measureTime(addrport, func() error {
				conn, err := dialer.Dial("tcp", addrport)
				if err != nil {
					if errors.Is(err, syscall.ETIMEDOUT) {
						slog.Warn("connection timeout", "addr", addrport)
						return nil
					}
					return fmt.Errorf("dialing %q: %w", addrport, err)
				}
				defer conn.Close()

				if err := sock.SetLinger(conn); err != nil {
					return fmt.Errorf("setting linger: %w", err)
				}
				if err := sock.SetQuickAck(conn); err != nil {
					return fmt.Errorf("setting quick ack: %w", err)
				}

				msg := bufTCPPool.Get().([]byte)
				defer bufTCPPool.Put(msg)

				if n, err := rand.Read(msg); err != nil {
					return fmt.Errorf("generating random data (length:%d): %w", n, err)
				}

				if _, err := conn.Write(msg); err != nil {
					if errors.Is(err, syscall.EINPROGRESS) {
						slog.Warn("write in progress", "addr", addrport)
						return nil
					}
					return fmt.Errorf("writing to connection: %w", err)
				}

				if _, err := conn.Read(msg); err != nil {
					if errors.Is(err, syscall.ECONNRESET) {
						slog.Warn("connection reset", "addr", addrport)
						return nil
					}
					return fmt.Errorf("reading from connection: %w", err)
				}

				return nil
			})
		})
	}
	return eg.Wait()
}

func connectUDP(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	connTotal := int64(connectRate) * int64(duration.Seconds())
	limiter := ratelimit.New(int(connectRate))

	bufUDPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	eg, ctx := errgroup.WithContext(ctx)
	for i := int64(0); i < connTotal; i++ {
		if err := waitLim(ctx, limiter); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			continue
		}

		eg.Go(func() error {
			return measureTime(addrport, func() error {
				conn, err := net.Dial("udp4", addrport)
				if err != nil {
					return fmt.Errorf("dialing UDP %q: %w", addrport, err)
				}
				defer conn.Close()

				msg := bufUDPPool.Get().([]byte)
				defer bufUDPPool.Put(msg)

				if n, err := rand.Read(msg); err != nil {
					return fmt.Errorf("generating random data (length:%d): %w", n, err)
				}

				if _, err := conn.Write(msg); err != nil {
					return fmt.Errorf("writing to UDP connection: %w", err)
				}

				if _, err := conn.Read(msg); err != nil {
					return fmt.Errorf("reading from UDP connection: %w", err)
				}

				return nil
			})
		})
	}
	return eg.Wait()
}
