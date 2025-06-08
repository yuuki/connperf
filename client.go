package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	ConnectRate          int32
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
		New: func() any { return make([]byte, c.config.MessageBytes) },
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

			msgsTotal := int64(c.config.ConnectRate) * int64(c.config.Duration.Seconds())
			limiter := ratelimit.New(int(c.config.ConnectRate))

			for j := int64(0); j < msgsTotal; j++ {
				if err := waitLim(ctx, limiter); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return nil
					}
					continue
				}

				if err := measureTime(addrport, c.config.MergeResultsEachHost, func() error {
					msg := bufTCPPool.Get().([]byte)
					defer bufTCPPool.Put(msg)

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
		New: func() any { return make([]byte, c.config.MessageBytes) },
	}

	dialer := net.Dialer{
		Control: GetTCPControlWithFastOpen(),
	}

	connTotal := int64(c.config.ConnectRate) * int64(c.config.Duration.Seconds())
	limiter := ratelimit.New(int(c.config.ConnectRate))

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

				if err := SetLinger(conn); err != nil {
					return fmt.Errorf("setting linger: %w", err)
				}
				if err := SetQuickAck(conn); err != nil {
					return fmt.Errorf("setting quick ack: %w", err)
				}

				msg := bufTCPPool.Get().([]byte)
				defer bufTCPPool.Put(msg)

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

	connTotal := int64(c.config.ConnectRate) * int64(c.config.Duration.Seconds())
	limiter := ratelimit.New(int(c.config.ConnectRate))

	bufUDPPool := sync.Pool{
		New: func() any { return make([]byte, c.config.MessageBytes) },
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

				msg := bufUDPPool.Get().([]byte)
				defer bufUDPPool.Put(msg)

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

func toMicroseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}

func toMicrosecondsf(n float64) int64 {
	return time.Duration(n).Microseconds()
}

func printStatHeader(w io.Writer) {
	fmt.Fprintf(w, "%-20s %-10s %-15s %-15s %-15s %-15s %-15s %-15s %-10s\n",
		"PEER", "CNT", "LAT_MAX(µs)", "LAT_MIN(µs)", "LAT_MEAN(µs)",
		"LAT_90p(µs)", "LAT_95p(µs)", "LAT_99p(µs)", "RATE(/s)")
}

func printStatLine(w io.Writer, addr string, stat metrics.Timer) {
	fmt.Fprintf(w, "%-20s %-10d %-15d %-15d %-15d %-15d %-15d %-15d %-10.2f\n",
		addr,
		stat.Count(),
		toMicroseconds(stat.Max()),
		toMicroseconds(stat.Min()),
		toMicrosecondsf(stat.Mean()),
		toMicrosecondsf(stat.Percentile(0.9)),
		toMicrosecondsf(stat.Percentile(0.95)),
		toMicrosecondsf(stat.Percentile(0.99)),
		stat.RateMean(),
	)
}

func runStatLinePrinter(ctx context.Context, w io.Writer, addr string, intervalStats time.Duration, mergeResultsEachHost bool) {
	go func() {
		ticker := time.NewTicker(intervalStats)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts := getOrRegisterTimer("tick.latency", addr, mergeResultsEachHost)
				printStatLine(w, addr, ts)
				unregisterTimer("tick.latency", addr, mergeResultsEachHost)
			}
		}
	}()
}

type JSONLinesResult struct {
	Peer        string  `json:"peer"`
	Count       int64   `json:"count"`
	LatencyMax  int64   `json:"latency_max_us"`
	LatencyMin  int64   `json:"latency_min_us"`
	LatencyMean int64   `json:"latency_mean_us"`
	Latency90p  int64   `json:"latency_90p_us"`
	Latency95p  int64   `json:"latency_95p_us"`
	Latency99p  int64   `json:"latency_99p_us"`
	Rate        float64 `json:"rate_per_sec"`
	Timestamp   string  `json:"timestamp"`
}

func printJSONLinesReport(w io.Writer, addrs []string, mergeResultsEachHost bool) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	
	if mergeResultsEachHost {
		ts := getOrRegisterTimer("total.latency", "", mergeResultsEachHost)
		result := JSONLinesResult{
			Peer:        fmt.Sprintf("merged(%d hosts)", len(addrs)),
			Count:       ts.Count(),
			LatencyMax:  toMicroseconds(ts.Max()),
			LatencyMin:  toMicroseconds(ts.Min()),
			LatencyMean: toMicrosecondsf(ts.Mean()),
			Latency90p:  toMicrosecondsf(ts.Percentile(0.9)),
			Latency95p:  toMicrosecondsf(ts.Percentile(0.95)),
			Latency99p:  toMicrosecondsf(ts.Percentile(0.99)),
			Rate:        ts.RateMean(),
			Timestamp:   timestamp,
		}
		json.NewEncoder(w).Encode(result)
		return
	}
	
	for _, addr := range addrs {
		ts := getOrRegisterTimer("total.latency", addr, mergeResultsEachHost)
		result := JSONLinesResult{
			Peer:        addr,
			Count:       ts.Count(),
			LatencyMax:  toMicroseconds(ts.Max()),
			LatencyMin:  toMicroseconds(ts.Min()),
			LatencyMean: toMicrosecondsf(ts.Mean()),
			Latency90p:  toMicrosecondsf(ts.Percentile(0.9)),
			Latency95p:  toMicrosecondsf(ts.Percentile(0.95)),
			Latency99p:  toMicrosecondsf(ts.Percentile(0.99)),
			Rate:        ts.RateMean(),
			Timestamp:   timestamp,
		}
		json.NewEncoder(w).Encode(result)
	}
}

func printReport(w io.Writer, addrs []string, mergeResultsEachHost bool) {
	fmt.Fprintln(w, "--- A result during total execution time ---")
	if mergeResultsEachHost {
		ts := getOrRegisterTimer("total.latency", "", mergeResultsEachHost)
		printStatLine(w, fmt.Sprintf("merged(%d hosts)", len(addrs)), ts)
		return
	}
	for _, addr := range addrs {
		ts := getOrRegisterTimer("total.latency", addr, mergeResultsEachHost)
		printStatLine(w, addr, ts)
	}
}