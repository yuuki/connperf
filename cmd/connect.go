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
	"log"
	"net"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	connections int32
	connectRate int32
	duration    time.Duration
)

// connectCmd represents the connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect connects to a port where 'serve' listens",
	RunE: func(cmd *cobra.Command, args []string) error {
		return connect(cmd.Flags().Arg(0))
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
	connectCmd.Flags().Int32VarP(&connections, "connections", "c", 10, "Number of connections to keep")
	connectCmd.Flags().Int32VarP(&connectRate, "rate", "r", 100, "connections throughput (/s)")
	connectCmd.Flags().DurationVarP(&duration, "duration", "d", 10*time.Second, "measurement period")
}

func connect(addrport string) error {
	wg := &sync.WaitGroup{}
	var i int32
	for i = 0; i < connections; i++ {
		wg.Add(1)
		go func() {
			conn, err := net.Dial("tcp", addrport)
			if err != nil {
				log.Printf("could not dial %q: %s", addrport, err)
			}
			if _, err := conn.Write([]byte("Hello")); err != nil {
				log.Printf("could not write: %s\n", err)
			}

			timer := time.NewTimer(duration)
			<-timer.C

			if err := conn.Close(); err != nil {
				log.Printf("could not close: %s\n", err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}
