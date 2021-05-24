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
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/spf13/cobra"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

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
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect connects to a port where 'serve' listens",
	Args: func(cmd *cobra.Command, args []string) error {
		switch connectFlavor {
		case flavorPersistent:
		case flavorEphemeral:
		default:
			return fmt.Errorf("unexpected connect flavor %q", connectFlavor)
		}

		switch protocol {
		case "tcp":
		case "udp":
		default:
			return fmt.Errorf("unexpected protocol %q", protocol)
		}

		if len(args) < 1 {
			return fmt.Errorf("required addresses")
		}

		if mergeResultsEachHost && !showOnlyResults {
			return fmt.Errorf("--merge-results-each-host flag requires --show-only-results flag")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		setPprofServer()
		if err := runConnectCmd(cmd, args); err != nil {
			cmd.PrintErrf("%v\n", err)
		}
	},
}

func init() {
	log.SetOutput(connectCmd.OutOrStderr())

	rootCmd.AddCommand(connectCmd)

	connectCmd.Flags().StringVarP(&protocol, "proto", "p", "tcp", "protocol (tcp or udp)")
	i := connectCmd.Flags().IntP("interval", "i", 5, "interval for printing stats")
	intervalStats = time.Duration(*i) * time.Second
	connectCmd.Flags().StringVarP(&connectFlavor, "flavor", "f", flavorPersistent,
		fmt.Sprintf("connect behavior type '%s' or '%s'", flavorPersistent, flavorEphemeral),
	)
	connectCmd.Flags().Int32VarP(&connections, "connections", "c", 10,
		fmt.Sprintf("Number of concurrent connections to keep (only for '%s')l", flavorPersistent))
	connectCmd.Flags().Int32VarP(&connectRate, "rate", "r", 100,
		fmt.Sprintf("New connections throughput (/s) (only for '%s')", flavorEphemeral))
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "measurement period")
	connectCmd.Flags().Int32Var(&messageBytes, "message-bytes", 64, "TCP/UDP message size (bytes)")
	connectCmd.Flags().BoolVar(&showOnlyResults, "show-only-results", false, "print only results of measurement stats")
	connectCmd.Flags().BoolVar(&mergeResultsEachHost, "merge-results-each-host", false, "merge results of each host (with --show-only-results)")

	connectCmd.Flags().BoolVar(&pprof, "enable-pprof", false, "a flag of pprof")
	connectCmd.Flags().StringVar(&pprofAddr, "pporf", "localhost:6060", "pprof listening address:port")
}

func setPprofServer() {
	if !pprof {
		return
	}
	go func() {
		log.Println(http.ListenAndServe(pprofAddr, nil))
	}()
}

func waitLim(ctx context.Context, rl ratelimit.Limiter) error {
	// Check if ctx is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	done := make(chan struct{})
	go func() {
		rl.Take()
		done <- struct{}{}
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Context was canceled before Take() task could be processed.
		return ctx.Err()
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	if err := limit.SetRLimitNoFile(); err != nil {
		return err
	}

	printStatHeader(cmd.OutOrStdout())

	done := make(chan error)
	go func() {
		eg, ctx := errgroup.WithContext(ctx)
		for _, addr := range args {
			addr := addr
			eg.Go(func() error {
				if showOnlyResults {
					if err := connectAddr(ctx, addr); err != nil {
						return err
					}
				} else {
					runStatLinePrinter(ctx, cmd.OutOrStdout(), addr)
					if err := connectAddr(ctx, addr); err != nil {
						return err
					}
				}
				return nil
			})
		}
		done <- eg.Wait()
	}()

	select {
	case <-ctx.Done():
		break
	case err := <-done:
		if err != nil {
			return err
		}
	}
	printReport(cmd.OutOrStdout(), args)

	return nil
}

func connectAddr(ctx context.Context, addr string) error {
	switch protocol {
	case "tcp":
		switch connectFlavor {
		case flavorPersistent:
			if err := connectPersistent(ctx, addr); err != nil {
				return err
			}
		case flavorEphemeral:
			if err := connectEphemeral(ctx, addr); err != nil {
				return err
			}
		}
	case "udp":
		if err := connectUDP(ctx, addr); err != nil {
			return err
		}
	}

	return nil
}

func toMicroseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}
func toMicrosecondsf(n float64) int64 {
	return time.Duration(n).Microseconds()
}

func printStatHeader(w io.Writer) {
	fmt.Printf("%-20s %-10s %-15s %-15s %-15s %-15s %-15s %-15s %-10s\n",
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
		t := time.NewTicker(intervalStats)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				is := getOrRegisterTimer("tick.latency", addr)
				printStatLine(w, addr, is)
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
	ts.UpdateSince(start)
	is.UpdateSince(start)
	return nil
}

func connectPersistent(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	bufTCPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	cause := make(chan error, 1)
	go func() {
		wg := &sync.WaitGroup{}
		for i := 0; i < int(connections); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				conn, err := net.Dial("tcp", addrport)
				if err != nil {
					cause <- xerrors.Errorf("could not dial %q: %w", addrport, err)
					cancel()
					return
				}
				defer conn.Close()

				msgsTotal := int64(connectRate) * int64(duration.Seconds())
				limiter := ratelimit.New(int(connectRate))

				for j := int64(0); j < msgsTotal; j++ {
					if err := waitLim(ctx, limiter); err != nil {
						if errors.Is(err, context.Canceled) ||
							errors.Is(err, context.DeadlineExceeded) {
							break
						}
						continue
					}

					err = measureTime(addrport, func() error {
						msg := bufTCPPool.Get().([]byte)
						defer func() { bufTCPPool.Put(msg) }()
						if n, err := rand.Read(msg); err != nil {
							return xerrors.Errorf("could not read random values (length:%d): %w", n, err)
						}

						if _, err := conn.Write(msg); err != nil {
							return xerrors.Errorf("could not write: %w", err)
						}
						if _, err := conn.Read(msg); err != nil {
							return xerrors.Errorf("could not read: %w", err)
						}
						return nil
					})
					if err != nil {
						cause <- err
						cancel()
					}
				}
			}()
		}
		wg.Wait()
		close(cause)
	}()

	select {
	case <-ctx.Done():
	case e := <-cause:
		return e
	}

	return nil
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

	cause := make(chan error, 1)
	go func() {
		wg := sync.WaitGroup{}
		for i := int64(0); i < connTotal; i++ {
			if err := waitLim(ctx, limiter); err != nil {
				if errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded) {
					break
				}
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				// start timer of measuring latency
				err := measureTime(addrport, func() error {
					conn, err := dialer.Dial("tcp", addrport)
					if err != nil {
						return xerrors.Errorf("could not dial %q: %w", addrport, err)
					}
					if err := sock.SetLinger(conn); err != nil {
						return err
					}
					if err := sock.SetQuickAck(conn); err != nil {
						return err
					}

					msg := bufTCPPool.Get().([]byte)
					defer func() { bufTCPPool.Put(msg) }()
					if n, err := rand.Read(msg); err != nil {
						return xerrors.Errorf("could not read random values (length:%d): %w", n, err)
					}

					if _, err := conn.Write(msg); err != nil {
						return xerrors.Errorf("could not write %q: %w", addrport, err)
					}
					if _, err := conn.Read(msg); err != nil {
						return xerrors.Errorf("could not read %q: %w", addrport, err)
					}

					if err := conn.Close(); err != nil {
						return xerrors.Errorf("could not close %q: %w", addrport, err)
					}

					return nil
				})
				if err != nil {
					cause <- err
					cancel()
				}
			}()
		}
		wg.Wait()
		close(cause)
	}()

	select {
	case <-ctx.Done():
		break
	case e := <-cause:
		return e
	}
	return nil
}

func connectUDP(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	connTotal := int64(connectRate) * int64(duration.Seconds())
	limiter := ratelimit.New(int(connectRate))

	bufUDPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	cause := make(chan error, 1)
	go func() {
		wg := sync.WaitGroup{}
		for i := int64(0); i < connTotal; i++ {
			if err := waitLim(ctx, limiter); err != nil {
				if errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded) {
					break
				}
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				// start timer of measuring latency
				err := measureTime(addrport, func() error {
					// create socket
					conn, err := net.Dial("udp4", addrport)
					if err != nil {
						return xerrors.Errorf("could not dial %q: %w", addrport, err)
					}
					defer conn.Close()

					msg := bufUDPPool.Get().([]byte)
					defer func() { bufUDPPool.Put(msg) }()
					if n, err := rand.Read(msg); err != nil {
						return xerrors.Errorf("could not read random values (length:%d): %w", n, err)
					}

					if _, err := conn.Write(msg); err != nil {
						return xerrors.Errorf("could not write %q: %w", addrport, err)
					}
					if _, err := conn.Read(msg); err != nil {
						return xerrors.Errorf("could not read %q: %w", addrport, err)
					}
					return nil
				})
				if err != nil {
					cause <- err
					cancel()
				}
			}()
		}
		wg.Wait()
		close(cause)
	}()

	select {
	case <-ctx.Done():
		break
	case e := <-cause:
		return e
	}

	return nil
}
