package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	UDPPacketSize     = 1500
	TCPBufferSize     = 4 * 1024
	RetryDelaySeconds = 1
)

var serveMsgBuf = sync.Pool{
	New: func() any {
		buf := make([]byte, TCPBufferSize)
		return &buf
	},
}

var bufUDPPool = sync.Pool{
	New: func() any {
		buf := make([]byte, UDPPacketSize)
		return &buf
	},
}

type ServerConfig struct {
	ListenAddrs []string
	Protocol    string
}

type Server struct {
	config ServerConfig
}

func NewServer(config ServerConfig) *Server {
	return &Server{config: config}
}

func (s *Server) Start(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	if s.config.Protocol == "tcp" || s.config.Protocol == "all" {
		eg.Go(func() error {
			if err := s.serveTCP(ctx); err != nil {
				slog.Error("TCP server error", "error", err)
				return err
			}
			return nil
		})
	}
	if s.config.Protocol == "udp" || s.config.Protocol == "all" {
		eg.Go(func() error {
			if err := s.serveUDP(ctx); err != nil {
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
}

func (s *Server) serveTCP(ctx context.Context) error {
	lc := net.ListenConfig{
		Control: GetTCPControlWithFastOpen(),
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range s.config.ListenAddrs {
		addr := listenAddr
		eg.Go(func() error {
			ln, err := lc.Listen(ctx, "tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %q error: %w", addr, err)
			}
			defer ln.Close()

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
					if errors.As(err, &ne) && ne.Timeout() {
						slog.Warn("timeout error accepting TCP connection",
							"addr", ln.Addr(),
							"error", err)
						time.Sleep(RetryDelaySeconds * time.Second)
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

	bufPtr := serveMsgBuf.Get().(*[]byte)
	defer serveMsgBuf.Put(bufPtr)
	buf := *bufPtr

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// // Check for connection reset by peer - don't treat as error
			// if isConnectionReset(err) {
			// 	return nil
			// }
			return fmt.Errorf("reading from %q: %w", conn.RemoteAddr(), err)
		}

		if _, err := conn.Write(buf[:n]); err != nil {
			if err == io.EOF {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// Check for connection reset by peer - don't treat as error
			// if isConnectionReset(err) {
			// 	return nil
			// }
			return fmt.Errorf("writing to %q: %w", conn.RemoteAddr(), err)
		}
	}
}

func (s *Server) serveUDP(ctx context.Context) error {
	lc := net.ListenConfig{}

	eg, ctx := errgroup.WithContext(ctx)
	for _, listenAddr := range s.config.ListenAddrs {
		addr := listenAddr
		eg.Go(func() error {
			ln, err := lc.ListenPacket(ctx, "udp4", addr)
			if err != nil {
				return fmt.Errorf("listen %q error: %w", addr, err)
			}
			defer ln.Close()

			go func() {
				<-ctx.Done()
				ln.Close()
			}()

			for {
				msgPtr := bufUDPPool.Get().(*[]byte)
				msg := *msgPtr
				n, remoteAddr, err := ln.ReadFrom(msg)
				if err != nil {
					bufUDPPool.Put(msgPtr)
					select {
					case <-ctx.Done():
						return nil
					default:
					}
					slog.Error("UDP read error", "error", err)
					continue
				}

				go func(msgPtr *[]byte, msg []byte) {
					defer bufUDPPool.Put(msgPtr)
					if _, err = ln.WriteTo(msg[:n], remoteAddr); err != nil {
						slog.Error("UDP write error",
							"remote_addr", remoteAddr,
							"error", err)
					}
				}(msgPtr, msg)
			}
		})
	}

	return eg.Wait()
}
