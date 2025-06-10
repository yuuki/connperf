package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/rcrowley/go-metrics"
	"go.uber.org/ratelimit"
	"golang.org/x/sync/errgroup"
)

const (
	flavorPersistent = "persistent"
	flavorEphemeral  = "ephemeral"
)

type ClientConfig struct {
	Protocol             string
	ConnectFlavor        string
	Connections          int32
	Rate                 int32
	Duration             time.Duration
	MessageBytes         int32
	MergeResultsEachHost bool
	JSONLines            bool
}

type Client struct {
	config ClientConfig
}

func NewClient(config ClientConfig) *Client {
	return &Client{config: config}
}

func waitLim(ctx context.Context, rl ratelimit.Limiter) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		done := make(chan struct{})
		go func() {
			rl.Take()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func getOrRegisterTimer(key, addr string, mergeResultsEachHost bool) metrics.Timer {
	if mergeResultsEachHost {
		return metrics.GetOrRegisterTimer(key, nil)
	}
	return metrics.GetOrRegisterTimer(key+"."+addr, nil)
}

func unregisterTimer(key, addr string, mergeResultsEachHost bool) {
	if mergeResultsEachHost {
		metrics.Unregister(key)
		return
	}
	metrics.Unregister(key + "." + addr)
}

func measureTime(addr string, mergeResultsEachHost bool, f func() error) error {
	ts := getOrRegisterTimer("total.latency", addr, mergeResultsEachHost)
	is := getOrRegisterTimer("tick.latency", addr, mergeResultsEachHost)
	start := time.Now()
	if err := f(); err != nil {
		return err
	}
	elapsed := time.Since(start)
	ts.Update(elapsed)
	is.Update(elapsed)
	return nil
}

func (c *Client) ConnectToAddresses(ctx context.Context, addrs []string) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, addr := range addrs {
		addr := addr
		eg.Go(func() error {
			return c.connectAddr(ctx, addr)
		})
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	return nil
}

func (c *Client) connectAddr(ctx context.Context, addr string) error {
	switch c.config.Protocol {
	case "tcp":
		switch c.config.ConnectFlavor {
		case flavorPersistent:
			return c.connectPersistent(ctx, addr)
		case flavorEphemeral:
			return c.connectEphemeral(ctx, addr)
		}
	case "udp":
		return c.connectUDP(ctx, addr)
	}
	return fmt.Errorf("invalid protocol or flavor combination")
}

func (c *Client) connectPersistent(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Duration)
	defer cancel()

	bufTCPPool := sync.Pool{
		New: func() any {
			buf := make([]byte, c.config.MessageBytes)
			return &buf
		},
	}

	dialer := net.Dialer{
		Control: GetTCPControlWithFastOpen(),
	}

	eg, ctx := errgroup.WithContext(ctx)
	for i := 0; i < int(c.config.Connections); i++ {
		eg.Go(func() error {
			conn, err := dialer.Dial("tcp", addrport)
			if err != nil {
				return fmt.Errorf("dialing %q: %w", addrport, err)
			}
			defer conn.Close()

			msgsTotal := int64(c.config.Rate) * int64(c.config.Duration.Seconds())
			limiter := ratelimit.New(int(c.config.Rate))

			for j := int64(0); j < msgsTotal; j++ {
				if err := waitLim(ctx, limiter); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil
					}
					continue
				}

				if err := measureTime(addrport, c.config.MergeResultsEachHost, func() error {
					msgPtr := bufTCPPool.Get().(*[]byte)
					msg := *msgPtr
					defer bufTCPPool.Put(msgPtr)

					if n, err := rand.Read(msg); err != nil {
						return fmt.Errorf("generating random data (length:%d): %w", n, err)
					}

					if _, err := conn.Write(msg); err != nil {
						return fmt.Errorf("writing to connection: %w", err)
					}
					if _, err := conn.Read(msg); err != nil {
						return fmt.Errorf("reading from connection: %w", err)
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return eg.Wait()
}

func (c *Client) connectEphemeral(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Duration)
	defer cancel()

	bufTCPPool := sync.Pool{
		New: func() any {
			buf := make([]byte, c.config.MessageBytes)
			return &buf
		},
	}

	dialer := net.Dialer{
		Control: GetTCPControlWithFastOpen(),
	}

	connTotal := int64(c.config.Rate) * int64(c.config.Duration.Seconds())
	limiter := ratelimit.New(int(c.config.Rate))

	eg, ctx := errgroup.WithContext(ctx)
	for i := int64(0); i < connTotal; i++ {
		if err := waitLim(ctx, limiter); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			continue
		}

		eg.Go(func() error {
			return measureTime(addrport, c.config.MergeResultsEachHost, func() error {
				conn, err := dialer.Dial("tcp", addrport)
				if err != nil {
					if errors.Is(err, syscall.ETIMEDOUT) {
						slog.Warn("connection timeout", "addr", addrport)
						return nil
					}
					return fmt.Errorf("dialing %q: %w", addrport, err)
				}
				defer conn.Close()

				if err := SetQuickAck(conn); err != nil {
					return fmt.Errorf("setting quick ack: %w", err)
				}

				msgPtr := bufTCPPool.Get().(*[]byte)
				msg := *msgPtr
				defer bufTCPPool.Put(msgPtr)

				if n, err := rand.Read(msg); err != nil {
					return fmt.Errorf("generating random data (length:%d): %w", n, err)
				}

				if _, err := conn.Write(msg); err != nil {
					if errors.Is(err, syscall.EINPROGRESS) {
						slog.Warn("write in progress", "addr", addrport)
						return nil
					}
					return fmt.Errorf("writing to connection: %w", err)
				}

				if _, err := conn.Read(msg); err != nil {
					if errors.Is(err, syscall.ECONNRESET) {
						slog.Warn("connection reset", "addr", addrport)
						return nil
					}
					return fmt.Errorf("reading from connection: %w", err)
				}

				return nil
			})
		})
	}
	return eg.Wait()
}

func (c *Client) connectUDP(ctx context.Context, addrport string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.Duration)
	defer cancel()

	connTotal := int64(c.config.Rate) * int64(c.config.Duration.Seconds())
	limiter := ratelimit.New(int(c.config.Rate))

	bufUDPPool := sync.Pool{
		New: func() any {
			buf := make([]byte, c.config.MessageBytes)
			return &buf
		},
	}

	eg, ctx := errgroup.WithContext(ctx)
	for i := int64(0); i < connTotal; i++ {
		if err := waitLim(ctx, limiter); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			continue
		}

		eg.Go(func() error {
			return measureTime(addrport, c.config.MergeResultsEachHost, func() error {
				conn, err := net.Dial("udp4", addrport)
				if err != nil {
					return fmt.Errorf("dialing UDP %q: %w", addrport, err)
				}
				defer conn.Close()

				msgPtr := bufUDPPool.Get().(*[]byte)
				msg := *msgPtr
				defer bufUDPPool.Put(msgPtr)

				if n, err := rand.Read(msg); err != nil {
					return fmt.Errorf("generating random data (length:%d): %w", n, err)
				}

				if _, err := conn.Write(msg); err != nil {
					return fmt.Errorf("writing to UDP connection: %w", err)
				}

				if _, err := conn.Read(msg); err != nil {
					return fmt.Errorf("reading from UDP connection: %w", err)
				}

				return nil
			})
		})
	}
	return eg.Wait()
}

func runStatLinePrinter(ctx context.Context, printer *Printer, addr string, intervalStats time.Duration, mergeResultsEachHost bool) {
	go func() {
		ticker := time.NewTicker(intervalStats)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts := getOrRegisterTimer("tick.latency", addr, mergeResultsEachHost)
				printer.PrintStatLine(addr, ts)
				unregisterTimer("tick.latency", addr, mergeResultsEachHost)
			}
		}
	}()
}
