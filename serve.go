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
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

var (
	listenAddrs     []string
	listenAddrsFile string
	serveProtocol   string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve accepts connections",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := SetRLimitNoFile(); err != nil {
			return fmt.Errorf("setting file limit: %w", err)
		}

		ctx, stop := signal.NotifyContext(
			context.Background(), unix.SIGINT, unix.SIGTERM)
		defer stop()

		if listenAddrsFile != "" {
			addrs, err := getAddrsFromFile(listenAddrsFile)
			if err != nil {
				return fmt.Errorf("reading addresses from file: %w", err)
			}
			listenAddrs = addrs
		}

		cmd.Printf("Listening at %q ...\n", listenAddrs)

		eg, ctx := errgroup.WithContext(ctx)

		if serveProtocol == "tcp" || serveProtocol == "all" {
			eg.Go(func() error {
				if err := serveTCP(ctx); err != nil {
					slog.Error("TCP server error", "error", err)
					return err
				}
				return nil
			})
		}
		if serveProtocol == "udp" || serveProtocol == "all" {
			eg.Go(func() error {
				if err := serveUDP(ctx); err != nil {
					slog.Error("UDP server error", "error", err)
					return err
				}
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return fmt.Errorf("server error: %w", err)
		}

		slog.Info("Server shutdown complete")
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
	serveCmd.Flags().StringVar(&listenAddrsFile, "listen-addrs-file", "", "enable to pass a file including a pair of addresses and ports")
}

func serveTCP(ctx context.Context) error {
	lc := net.ListenConfig{
		Control: GetTCPControlWithFastOpen(),
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range listenAddrs {
		addr := listenAddr // Create new variable for closure
		eg.Go(func() error {
			ln, err := lc.Listen(ctx, "tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %q error: %w", addr, err)
			}
			defer ln.Close()

			// Close listener when context is done
			go func() {
				<-ctx.Done()
				ln.Close()
			}()

			for {
				conn, err := ln.Accept()
				if err != nil {
					select {
					case <-ctx.Done():
						return nil
					default:
					}

					var ne net.Error
					if errors.As(err, &ne) && ne.Temporary() {
						slog.Warn("temporary error accepting TCP connection",
							"addr", ln.Addr(),
							"error", err)
						time.Sleep(time.Second)
						continue
					}
					if errors.Is(err, net.ErrClosed) {
						return nil
					}
					return fmt.Errorf("accepting TCP connection: %w", err)
				}

				if err := SetQuickAck(conn); err != nil {
					return fmt.Errorf("setting quick ack: %w", err)
				}
				if err := SetLinger(conn); err != nil {
					return fmt.Errorf("setting linger: %w", err)
				}

				go func() {
					if err := handleConnection(conn); err != nil {
						slog.Error("connection handler error",
							"remote_addr", conn.RemoteAddr(),
							"error", err)
					}
				}()
			}
		})
	}
	return eg.Wait()
}

func handleConnection(conn net.Conn) error {
	defer conn.Close()

	buf := serveMsgBuf.Get().([]byte)
	defer serveMsgBuf.Put(buf)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("reading from %q: %w", conn.RemoteAddr(), err)
		}

		if _, err := conn.Write(buf[:n]); err != nil {
			if err == io.EOF {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("writing to %q: %w", conn.RemoteAddr(), err)
		}
	}
}

const UDPPacketSize = 1500

var bufUDPPool = sync.Pool{
	New: func() interface{} { return make([]byte, UDPPacketSize) },
}

func serveUDP(ctx context.Context) error {
	lc := net.ListenConfig{}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range listenAddrs {
		addr := listenAddr // Create new variable for closure
		eg.Go(func() error {
			ln, err := lc.ListenPacket(ctx, "udp4", addr)
			if err != nil {
				return fmt.Errorf("listen %q error: %w", addr, err)
			}
			defer ln.Close()

			// Close listener when context is done
			go func() {
				<-ctx.Done()
				ln.Close()
			}()

			for {
				msg := bufUDPPool.Get().([]byte)
				n, remoteAddr, err := ln.ReadFrom(msg)
				if err != nil {
					bufUDPPool.Put(msg)
					select {
					case <-ctx.Done():
						return nil
					default:
					}
					slog.Error("UDP read error", "error", err)
					continue
				}

				go func() {
					defer bufUDPPool.Put(msg)
					if _, err = ln.WriteTo(msg[:n], remoteAddr); err != nil {
						slog.Error("UDP write error",
							"remote_addr", remoteAddr,
							"error", err)
					}
				}()
			}
		})
	}

	return eg.Wait()
}
