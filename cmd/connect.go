/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

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
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

const (
	connectTypePersistent = "persistent"
	connectTypeEphemeral  = "ephemeral"
)

type reportStats struct {
	ConnectionsTotal int64
}

func (s reportStats) Print(w io.Writer) {
	fmt.Fprintf(w, "--- Execution results of connperf ---\n")
	fmt.Fprintf(w, "Total number of connections: %d\n", s.ConnectionsTotal)
}

var (
	udp bool

	connectType string
	connections int32
	connectRate int32
	duration    time.Duration

	stats reportStats
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect connects to a port where 'serve' listens",
	Args: func(cmd *cobra.Command, args []string) error {
		switch connectType {
		case connectTypePersistent:
		case connectTypeEphemeral:
		default:
			return fmt.Errorf("undefined connect mode %q", connectType)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if udp {
			addr := cmd.Flags().Arg(0)
			if err := connectUDP(addr); err != nil {
				return err
			}
			stats.Print(cmd.OutOrStdout())
			return nil
		}
		switch connectType {
		case connectTypePersistent:
			addr := cmd.Flags().Arg(0)
			cmd.Printf("Trying to connect to %q with %q connections (connections: %d, duration: %s)...\n",
				addr, connectTypePersistent, connections, duration)
			if err := connectPersistent(addr); err != nil {
				return err
			}
		case connectTypeEphemeral:
			addr := cmd.Flags().Arg(0)
			cmd.Printf("Trying to connect to %q with %q connections (rate: %d, duration: %s)\n",
				addr, connectTypeEphemeral, connectRate, duration)
			if err := connectEphemeral(addr); err != nil {
				return err
			}
		default:
			return fmt.Errorf("undefined connect mode %q", connectType)
		}
		stats.Print(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)

	connectCmd.Flags().BoolVar(&udp, "udp", false, "UDP mode")

	connectCmd.Flags().StringVar(&connectType, "type", connectTypePersistent,
		fmt.Sprintf("connect behavior type '%s' or '%s'", connectTypePersistent, connectTypeEphemeral),
	)
	connectCmd.Flags().Int32VarP(&connections, "connections", "c", 10,
		fmt.Sprintf("Number of connections to keep (only for '%s')l", connectTypePersistent))
	connectCmd.Flags().Int32VarP(&connectRate, "rate", "r", 100,
		fmt.Sprintf("New connections throughput (/s) (only for '%s')", connectTypeEphemeral))
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "Measurement period")
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

			atomic.AddInt64(&stats.ConnectionsTotal, 1)
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

	var i int32
	for i = 0; i < connTotal; i++ {
		if err := limiter.Wait(ctx); err != nil {
			log.Println(err)
			continue
		}
		go func() {
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

			atomic.AddInt64(&stats.ConnectionsTotal, 1)
		}()
	}

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

	var i int32
	for i = 0; i < connTotal; i++ {
		if err := limiter.Wait(ctx); err != nil {
			log.Println(err)
			continue
		}
		go func() {
			// create socket
			conn, err := net.Dial("udp", addrport)
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

			atomic.AddInt64(&stats.ConnectionsTotal, 1)
		}()
	}

	return nil
}
