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
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
	"golang.org/x/xerrors"

	"github.com/yuuki/connperf/limit"
)

const (
	flavorPersistent = "persistent"
	flavorEphemeral  = "ephemeral"
)

var (
	protocol        string
	intervalStats   time.Duration
	connectFlavor   string
	connections     int32
	connectRate     int32
	duration        time.Duration
	messageBytes    int32
	showOnlyResults bool
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

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
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
}

func printReport(w io.Writer, addrs []string) {
	fmt.Fprintln(w, "--- A result during total execution time ---")
	for _, addr := range addrs {
		ts := metrics.GetOrRegisterTimer("total.latency."+addr, nil)
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
				is := metrics.GetOrRegisterTimer("tick.latency."+addr, nil)
				printStatLine(w, addr, is)
				metrics.Unregister("tick.latency." + addr)
			}
		}
	}()
}

func meastureTime(addr string, f func() error) error {
	ts := metrics.GetOrRegisterTimer("total.latency."+addr, nil)
	is := metrics.GetOrRegisterTimer("tick.latency."+addr, nil)
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
				}
				defer conn.Close()

				err = meastureTime(addrport, func() error {
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

	connTotal := int64(connectRate) * int64(duration.Seconds())
	tr := rate.Every(time.Second / time.Duration(connectRate))
	limiter := rate.NewLimiter(tr, int(connectRate))

	cause := make(chan error, 1)
	go func() {
		wg := sync.WaitGroup{}
		for i := int64(0); i < connTotal; i++ {
			if err := limiter.Wait(ctx); err != nil {
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
				err := meastureTime(addrport, func() error {
					conn, err := net.Dial("tcp", addrport)
					if err != nil {
						return xerrors.Errorf("could not dial %q: %w", addrport, err)
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
						return xerrors.Errorf("could not write %q: %w", addrport, err)
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
	tr := rate.Every(time.Second / time.Duration(connectRate))
	limiter := rate.NewLimiter(tr, int(connectRate))

	bufUDPPool := sync.Pool{
		New: func() interface{} { return make([]byte, messageBytes) },
	}

	cause := make(chan error, 1)
	go func() {
		wg := sync.WaitGroup{}
		for i := int64(0); i < connTotal; i++ {
			if err := limiter.Wait(ctx); err != nil {
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
				err := meastureTime(addrport, func() error {
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
