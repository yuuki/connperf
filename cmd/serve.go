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
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yuuki/connperf/limit"
	"github.com/yuuki/connperf/sock"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

var (
	listenAddrs   []string
	serveProtocol string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve accepts connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := limit.SetRLimitNoFile(); err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(
			context.Background(), unix.SIGINT, unix.SIGTERM)
		defer stop()

		cmd.Printf("Listening at %q ...\n", listenAddrs)
		if serveProtocol == "tcp" || serveProtocol == "all" {
			go func() {
				if err := serveTCP(ctx); err != nil {
					log.Println(err)
				}
			}()
		}
		if serveProtocol == "udp" || serveProtocol == "all" {
			go func() {
				if err := serveUDP(ctx); err != nil {
					log.Println(err)
				}
			}()
		}

		ret := <-ctx.Done()
		log.Printf("Received %v, Goodbye\n", ret)

		return nil
	},
}

var serveMsgBuf = sync.Pool{
	New: func() interface{} { return make([]byte, 4*1024) },
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringSliceVarP(&listenAddrs, "listenAddr", "l", []string{"0.0.0.0:9100"}, "listening address")
	serveCmd.Flags().StringVarP(&serveProtocol, "protocol", "p", "all", "listening protocol ('tcp' or 'udp')")
}

func serveTCP(ctx context.Context) error {
	lc := net.ListenConfig{
		Control: sock.GetTCPControlWithFastOpen(),
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range listenAddrs {
		ln, err := lc.Listen(ctx, "tcp", listenAddr)
		if err != nil {
			return fmt.Errorf("listen %q error: %s", listenAddr, err)
		}
		eg.Go(func() error {
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
				if err := sock.SetQuickAck(conn); err != nil {
					return err
				}
				if err := sock.SetLinger(conn); err != nil {
					return err
				}
				go func() {
					if err := handleConnection(conn); err != nil {
						log.Println(err)
					}
				}()
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

func handleConnection(conn net.Conn) error {
	defer conn.Close()

	buf := serveMsgBuf.Get().([]byte)
	defer func() { serveMsgBuf.Put(buf) }()

	var done bool
	for !done {
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			if errors.Is(err, io.EOF) {
				done = true
			} else if errors.Is(err, syscall.ECONNRESET) {
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

func serveUDP(ctx context.Context) error {
	lc := net.ListenConfig{}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range listenAddrs {
		// create listening socket
		ln, err := lc.ListenPacket(ctx, "udp4", listenAddr)
		if err != nil {
			return fmt.Errorf("listen %q error: %s", listenAddr, err)
		}
		eg.Go(func() error {
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
		})
	}

	return nil
}
