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
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/spf13/cobra"
	"github.com/yuuki/connperf/limit"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
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
	RunE: runConnectCmd,
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
		fmt.Sprintf("Number of connections to keep (only for '%s')l", flavorPersistent))
	connectCmd.Flags().Int32VarP(&connectRate, "rate", "r", 100,
		fmt.Sprintf("New connections throughput (/s) (only for '%s')", flavorEphemeral))
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "measurement period")
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

	errChan := make(chan error)
	go func() {
		var eg errgroup.Group
		for _, addr := range args {
			addr := addr
			eg.Go(func() error {
				if showOnlyResults {
					if err := connectAddr(addr); err != nil {
						return err
					}
				} else {
					stopCh := make(chan struct{})
					defer close(stopCh)
					runStatLinePrinter(cmd.OutOrStdout(), addr, stopCh)
					if err := connectAddr(addr); err != nil {
						return err
					}
					stopCh <- struct{}{}
				}
				return nil
			})
		}
		errChan <- eg.Wait()
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		stop()
	}
	printReport(cmd.OutOrStdout(), args)

	return nil
}

func connectAddr(addr string) error {
	switch protocol {
	case "tcp":
		switch connectFlavor {
		case flavorPersistent:
			if err := connectPersistent(addr); err != nil {
				return err
			}
		case flavorEphemeral:
			if err := connectEphemeral(addr); err != nil {
				return err
			}
		}
	case "udp":
		if err := connectUDP(addr); err != nil {
			return err
		}
	}

	return nil
}

func toMilliseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}
func toMillisecondsf(n float64) int64 {
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
		toMilliseconds(stat.Max()),
		toMilliseconds(stat.Min()),
		toMillisecondsf(stat.Mean()),
		toMillisecondsf(stat.Percentile(0.9)),
		toMillisecondsf(stat.Percentile(0.95)),
		toMillisecondsf(stat.Percentile(0.99)),
		stat.Rate1(),
	)
}

func runStatLinePrinter(w io.Writer, addr string, stop chan struct{}) {
	go func() {
		t := time.NewTicker(intervalStats)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				is := metrics.GetOrRegisterTimer("tick.latency."+addr, nil)
				printStatLine(w, addr, is)
				metrics.Unregister("tick.latency." + addr)
			case <-stop:
				return
			}
		}
	}()
}

func meastureTime(addr string, f func()) {
	ts := metrics.GetOrRegisterTimer("total.latency."+addr, nil)
	is := metrics.GetOrRegisterTimer("tick.latency."+addr, nil)
	ts.Time(func() { is.Time(f) })
}

func updateStat(addr string, n time.Duration) {
	ts := metrics.GetOrRegisterTimer("total.latency."+addr, nil)
	ts.Update(n)

	is := metrics.GetOrRegisterTimer("tick.latency."+addr, nil)
	is.Update(n)
}

func connectPersistent(addrport string) error {
	wg := &sync.WaitGroup{}
	var i int32
	for i = 0; i < connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := net.Dial("tcp", addrport)
			if err != nil {
				log.Printf("could not dial %q: %s", addrport, err)
				return
			}
			if _, err := conn.Write([]byte("Hello")); err != nil {
				log.Printf("could not write: %s\n", err)
				return
			}

			timer := time.NewTimer(duration)
			<-timer.C

			if err := conn.Close(); err != nil {
				log.Printf("could not close: %s\n", err)
				return
			}

			updateStat(addrport, 1)
		}()
	}
	wg.Wait()
	return nil
}

func connectEphemeral(addrport string) error {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	connTotal := connectRate * int32(duration.Seconds())
	tr := rate.Every(time.Second / time.Duration(connectRate))
	limiter := rate.NewLimiter(tr, 1)

	wg := &sync.WaitGroup{}
	var i int32
	for i = 0; i < connTotal; i++ {
		if err := limiter.Wait(ctx); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) &&
				!strings.Contains(err.Error(), "would exceed context deadline") {
				log.Printf("rate limiter failed wait: %s\n", err)
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			// start timer of measuring latency
			meastureTime(addrport, func() {
				conn, err := net.Dial("tcp", addrport)
				if err != nil {
					log.Printf("could not dial %q: %s", addrport, err)
					return
				}
				if _, err := conn.Write([]byte("Hello")); err != nil {
					log.Printf("could not write: %s\n", err)
					return
				}
				if err := conn.Close(); err != nil {
					log.Printf("could not close: %s\n", err)
					return
				}
			})
		}()
	}
	wg.Wait()

	return nil
}

func connectUDP(addrport string) error {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	connTotal := connectRate * int32(duration.Seconds())
	tr := rate.Every(time.Second / time.Duration(connectRate))
	limiter := rate.NewLimiter(tr, 1)

	bufUDPPool := sync.Pool{
		New: func() interface{} { return make([]byte, UDPPacketSize) },
	}

	wg := &sync.WaitGroup{}
	var i int32
	for i = 0; i < connTotal; i++ {
		if err := limiter.Wait(ctx); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) &&
				!strings.Contains(err.Error(), "would exceed context deadline") {
				log.Println(err)
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			// start timer of measuring latency
			meastureTime(addrport, func() {
				// create socket
				conn, err := net.Dial("udp4", addrport)
				if err != nil {
					log.Printf("could not dial %q: %s", addrport, err)
					return
				}
				defer func() {
					if err := conn.Close(); err != nil {
						log.Printf("could not close: %s\n", err)
					}
				}()

				msg := bufUDPPool.Get().([]byte)
				defer func() { bufUDPPool.Put(msg) }()

				if _, err := conn.Write([]byte("Hello")); err != nil {
					log.Printf("could not write: %s\n", err)
					return
				}
				if _, err := conn.Read(msg); err != nil {
					log.Printf("could not read: %s\n", err)
					return
				}
			})
		}()
	}
	wg.Wait()

	return nil
}
