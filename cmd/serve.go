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
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/yuuki/connperf/limit"
	"golang.org/x/xerrors"
)

var (
	listenAddr string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve accepts connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := limit.SetRLimitNoFile(); err != nil {
			return err
		}

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

var serveMsgBuf = sync.Pool{
	New: func() interface{} { return make([]byte, 4*1024) },
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
				if errors.Is(err, net.ErrClosed) {
					break
				}
				log.Fatalf("unrecoverable error when accepting TCP connections: %s", err)
			}
			log.Fatalf("unexpected error when accepting TCP connections: %s", err)
		}
		go func() {
			if err := handleConnection(conn); err != nil {
				log.Println(err)
			}
		}()
	}

	return nil
}

func handleConnection(conn net.Conn) error {
	defer conn.Close()

	buf := serveMsgBuf.Get().([]byte)
	defer func() { serveMsgBuf.Put(buf) }()

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			if errors.Is(err, io.EOF) {
				// TODO: may miss some reading data
				return nil
			} else {
				return xerrors.Errorf("Could not read %q: %w", conn.RemoteAddr(), err)
			}
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				return nil
			}
			return xerrors.Errorf("Could not write %q: %w", conn.RemoteAddr(), err)
		}
	}
	return nil
}

const (
	UDPPacketSize = 1500
)

var bufUDPPool sync.Pool

func serveUDP() error {
	// create listening socket
	ln, err := net.ListenPacket("udp4", listenAddr)
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
