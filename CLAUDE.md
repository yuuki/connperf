# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

tcpulse is a TCP/UDP connection performance load generator written in Go. It provides two primary modes:
- **serve**: Acts as a server accepting TCP/UDP connections and echoing back data
- **connect**: Acts as a client generating load against target servers

## Build Commands

```bash
# Build the binary
make build

# Build Docker image
make docker/build

# Run tests
make test
# or
go test ./...
```

## Development Workflow with GitHub

1. Create a new branch for [feature/bug/refactor-name] from main branch.
2. Coding the feature/bug.
3. Add or fix tests.
4. Check the code quality with testing, linting and formatting.
5. Create a pull request.
6. Review the pull request and fix the comments.
7. Merge the pull request.

## Architecture

The project follows a simple flat structure with all Go files in the top-level directory:

- `main.go`: Entry point with flag-based CLI using pflag and viper (-c for client, -s for server)
- `server.go`: TCP/UDP server logic with context-based cancellation and errgroup for concurrent handling
- `client.go`: Client implementation with persistent/ephemeral connection modes, rate limiting, and latency measurements
- `option.go`: Default socket option implementations (non-Linux platforms)
- `option_linux.go`: Linux-specific socket optimizations using build tags
- `utils.go`: Utility functions for file descriptor limits and file reading
- `version.go`: Version information

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

The project includes test files (connect_test.go, serve_test.go, e2e_test.go). When adding new functionality, follow the existing test patterns and ensure all edge cases are covered, especially around connection handling and error scenarios.

## Development Notes

- File descriptor limits are automatically raised via utils.go
- Server gracefully handles signals (SIGINT, SIGTERM) for clean shutdown
- Error handling includes specific logic for network timeouts and connection resets
- Build uses Go modules with go 1.24 (toolchain go1.24)
- Platform-specific code uses build tags (+build linux / +build !linux)

## Dependencies

Key dependencies include:
- github.com/spf13/pflag: POSIX-style command-line flag parsing
- github.com/spf13/viper: Configuration management with flag binding
- github.com/rcrowley/go-metrics: Performance metrics
- go.uber.org/ratelimit: Rate limiting
- golang.org/x/sync/errgroup: Concurrent error handling
- golang.org/x/sys/unix: Linux system calls

## Additional Guidance

- All Go files are placed in the top-level directory
- Use build tags for platform-specific implementations
