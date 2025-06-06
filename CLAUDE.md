# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

connperf is a TCP/UDP connection performance load generator written in Go. It provides two primary modes:
- **serve**: Acts as a server accepting TCP/UDP connections and echoing back data
- **connect**: Acts as a client generating load against target servers

## Build Commands

```bash
# Build the binary
make build

# Build Docker image
make docker

# Run tests
go test ./...

# Run tests for specific package
go test ./cmd/
```

## Architecture

The project follows a clean CLI architecture using the Cobra framework:

- `main.go`: Entry point that delegates to cmd.Execute()
- `cmd/root.go`: Root Cobra command setup
- `cmd/serve.go`: Server implementation with TCP/UDP listeners using context-based cancellation and errgroup for concurrent handling
- `cmd/connect.go`: Client implementation with persistent/ephemeral connection modes, rate limiting, and latency measurements
- `limit/`: Package for setting file descriptor limits to handle high connection counts
- `sock/`: Platform-specific socket optimizations (Linux vs non-Linux with build tags)

## Key Design Patterns

**Connection Modes:**
- **Persistent**: Maintains long-lived connections and sends multiple messages per connection
- **Ephemeral**: Creates new connections for each request, useful for connection establishment testing

**Performance Optimizations:**
- Uses `sync.Pool` for buffer reuse to reduce GC pressure
- Platform-specific socket options (TCP_FASTOPEN, SO_REUSEPORT, TCP_QUICKACK on Linux)
- Rate limiting with go.uber.org/ratelimit
- Context-based cancellation throughout

**Metrics & Reporting:**
- Real-time latency statistics using github.com/rcrowley/go-metrics
- Configurable reporting intervals
- Support for merging results across multiple target hosts

## Testing

The project includes test files (cmd/serve_test.go). When adding new functionality, follow the existing test patterns and ensure all edge cases are covered, especially around connection handling and error scenarios.

## Development Notes

- File descriptor limits are automatically raised via the `limit` package
- Server gracefully handles signals (SIGINT, SIGTERM) for clean shutdown
- Error handling includes specific logic for network timeouts and connection resets
- Build uses Go modules with go 1.24.4

## Additional Guidance

- Place all go files into top-level directory