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
	"strings"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

const (
	flavorPersistent = "persistent"
	flavorEphemeral  = "ephemeral"
)

var (
	protocol      string
	intervalStats time.Duration
	connectFlavor string
	connections   int32
	connectRate   int32
	duration      time.Duration

	globalStats = metrics.NewRegisteredTimer("latency", nil)
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
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := cmd.Flags().Arg(0)

		stop := make(chan struct{})
		defer close(stop)
		done := make(chan struct{})
		defer close(done)

		switch protocol {
		case "tcp":
			switch connectFlavor {
			case flavorPersistent:
				cmd.Printf("Trying to connect to %q with %q connections (connections: %d, duration: %s)...\n",
					addr, flavorPersistent, connections, duration)
				printLineTick(cmd.OutOrStdout(), stop, done)
				if err := connectPersistent(addr); err != nil {
					return err
				}
			case flavorEphemeral:
				cmd.Printf("Trying to connect to %q with %q connections (rate: %d, duration: %s)\n",
					addr, flavorEphemeral, connectRate, duration)
				printLineTick(cmd.OutOrStdout(), stop, done)
				if err := connectEphemeral(addr); err != nil {
					return err
				}
			}
		case "udp":
			printLineTick(cmd.OutOrStdout(), stop, done)
			if err := connectUDP(addr); err != nil {
				return err
			}
		}
		stop <- struct{}{}
		<-done // wait for completing goroutine.
		return nil
	},
}

func init() {
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
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "Measurement period")
}

func toMilliseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}
func toMillisecondsf(n float64) int64 {
	return time.Duration(n).Microseconds()
}

func printHeader(w io.Writer) {
	fmt.Printf("%-10s %-15s %-15s %-15s %-15s %-15s %-15s %-10s\n",
		"CNT", "LAT_MAX(µs)", "LAT_MIN(µs)", "LAT_MEAN(µs)",
		"LAT_90p(µs)", "LAT_95p(µs)", "LAT_99p(µs)", "RATE(/s)")
}

func printLine(w io.Writer, stat metrics.Timer) {
	fmt.Fprintf(w, "%-10d %-15d %-15d %-15d %-15d %-15d %-15d %-10.2f\n",
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

func printLineTick(w io.Writer, stop chan struct{}, done chan struct{}) {
	printHeader(w)
	go func() {
		defer func() { done <- struct{}{} }()
		t := time.NewTicker(intervalStats)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				is := metrics.GetOrRegisterTimer("instant.latency", nil)
				printLine(w, is)
				metrics.Unregister("instant.latency")
			case <-stop:
				fmt.Fprintln(w, "--- A result during total execution time ---")
				printLine(w, globalStats)
				globalStats.Stop()
				return
			}
		}
	}()
}

func meastureTime(f func()) {
	is := metrics.GetOrRegisterTimer("instant.latency", nil)
	globalStats.Time(func() {
		is.Time(f)
	})
}

func updateStat(n time.Duration) {
	globalStats.Update(n)
	is := metrics.GetOrRegisterTimer("instant.latency", nil)
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

			updateStat(1)
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
			meastureTime(func() {
				conn, err := net.Dial("tcp", addrport)
				if err != nil {
					log.Printf("could not dial %q: %s", addrport, err)
				}
				if _, err := conn.Write([]byte("Hello")); err != nil {
					log.Printf("could not write: %s\n", err)
				}
				if err := conn.Close(); err != nil {
					log.Printf("could not close: %s\n", err)
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
			meastureTime(func() {
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
