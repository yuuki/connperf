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
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	listenAddr string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve accepts connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Printf("Listening at %q ...\n", listenAddr)
		go func() {
			if err := serveTCP(); err != nil {
				log.Println(err)
			}
		}()
		go func() {
			if err := serveUDP(); err != nil {
				log.Println(err)
			}
		}()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, os.Kill)
		ret := <-sig
		log.Printf("Received %v, Goodbye\n", ret)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVarP(&listenAddr, "listenAddr", "l", "0.0.0.0:9100", "listening address")
}

func serveTCP() error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %q error: %s", listenAddr, err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok {
				if ne.Temporary() {
					log.Printf("temporary error when listening for TCP addr %q: %s", ln.Addr(), err)
					time.Sleep(time.Second)
					continue
				}
				if strings.Contains(err.Error(), "use of closed network connection") {
					break
				}
				log.Fatalf("unrecoverable error when accepting TCP connections: %s", err)
			}
			log.Fatalf("unexpected error when accepting TCP Graphite connections: %s", err)
		}
		go func() {
			if err := echoStream(conn); err != nil {
				log.Println(err)
				if err := conn.Close(); err != nil {
					log.Printf("could not close: %s\n", err)
				}
			}
		}()
	}

	return nil
}

func echoStream(conn net.Conn) error {
	r := bufio.NewReader(conn)
	msg, err := ioutil.ReadAll(r)
	if err != nil {
		return fmt.Errorf("could not read: %s", err)
	}
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("could write %q", msg)
	}
	return nil
}

const (
	UDPPacketSize = 1500
)

var bufUDPPool sync.Pool

func serveUDP() error {
	ln, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %q error: %s", listenAddr, err)
	}
	defer ln.Close()

	bufUDPPool = sync.Pool{
		New: func() interface{} { return make([]byte, UDPPacketSize) },
	}

	for {
		msg := bufUDPPool.Get().([]byte)
		n, addr, err := ln.ReadFrom(msg[0:])
		if err != nil {
			log.Printf("UDP read error: %s", err)
			continue
		}

		go func() {
			n, err = ln.WriteTo(msg[:n], addr)
			if err != nil {
				log.Printf("UDP send error: %s\n", err)
				return
			}
			bufUDPPool.Put(msg)
		}()
	}

	return nil
}
