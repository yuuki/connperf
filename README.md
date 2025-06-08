# tcpulse

[![Test](https://github.com/yuuki/tcpulse/actions/workflows/test.yml/badge.svg)](https://github.com/yuuki/tcpulse/actions/workflows/test.yml)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/yuuki/tcpulse)

tcpulse is a high-performance TCP/UDP connection load generator and performance measurement tool written in Go.

## What is tcpulse?

tcpulse is a specialized tool designed to measure and analyze the performance characteristics of network connections. It operates in two primary modes:

- **Server mode (`-s/--server`)**: Acts as an echo server that accepts TCP/UDP connections and echoes back received data
- **Client mode (`-c/--client`)**: Generates configurable load against target servers and measures connection performance metrics

## Why use tcpulse?

Network performance testing is crucial for:

- **Load testing**: Validate server capacity and identify bottlenecks before production deployment
- **Connection establishment performance**: Measure how quickly new connections can be established (ephemeral mode)
- **Sustained connection performance**: Test throughput and latency of long-lived connections (persistent mode)
- **Protocol comparison**: Compare TCP vs UDP performance characteristics
- **Infrastructure validation**: Verify network infrastructure can handle expected traffic patterns
- **Performance regression testing**: Detect performance degradations in network services

## How it works

tcpulse provides two distinct connection patterns to simulate real-world usage:

### Persistent Connections
Maintains long-lived connections and sends multiple requests per connection. This simulates applications like web services with connection pooling or persistent database connections.

### Ephemeral Connections
Creates new connections for each request, immediately closing them afterward. This simulates scenarios like HTTP/1.0 or testing connection establishment overhead.

### Key Features

- **Real-time metrics**: Latency percentiles (90th, 95th, 99th), throughput, and connection counts
- **Rate limiting**: Control connection establishment rate to simulate realistic load patterns
- **Multi-target support**: Test multiple endpoints simultaneously and aggregate results
- **Protocol support**: Both TCP and UDP with optimized implementations
- **Platform optimizations**: Leverages Linux-specific optimizations (TCP_FASTOPEN, SO_REUSEPORT) when available
- **JSON Lines output**: Machine-readable output format for integration with monitoring and analysis tools

## Installation

### Pre-built Binaries

Download the latest pre-built binaries from the [GitHub Releases](https://github.com/yuuki/tcpulse/releases) page:

```bash
# Linux (x86_64)
curl -LO https://github.com/yuuki/tcpulse/releases/latest/download/tcpulse_linux_amd64.tar.gz
tar -xzf tcpulse_linux_amd64.tar.gz
sudo mv tcpulse /usr/local/bin/

# macOS (x86_64)
curl -LO https://github.com/yuuki/tcpulse/releases/latest/download/tcpulse_darwin_amd64.tar.gz
tar -xzf tcpulse_darwin_amd64.tar.gz
sudo mv tcpulse /usr/local/bin/

# macOS (Apple Silicon)
curl -LO https://github.com/yuuki/tcpulse/releases/latest/download/tcpulse_darwin_arm64.tar.gz
tar -xzf tcpulse_darwin_arm64.tar.gz
sudo mv tcpulse /usr/local/bin/
```

### Build from Source

Requirements:
- Go 1.22+ (see [go.mod](go.mod) for exact version requirements)
- Git

```bash
# Clone the repository
git clone https://github.com/yuuki/tcpulse.git
cd tcpulse

# Build using Make
make build

# Or build directly with Go
go build -o tcpulse .

# Install to $GOPATH/bin
go install .
```

### Verify Installation

```bash
tcpulse --help
```

## Usage

```shell-session
$ tcpulse --help
Usage: tcpulse [OPTIONS] <addresses...>

tcpulse is a concurrent TCP/UDP load generator that provides fine-grained, flow-level control

Modes:
  -c, --client    Run in client mode (connect to servers)
  -s, --server    Run in server mode (accept connections)

Options:
      --addrs-file                 [client mode] enable to pass a file including a pair of addresses and ports to an argument
  -c, --client                     run in client mode
      --connections int32          [client mode] Number of concurrent connections to keep (only for 'persistent') (default 10)
      --duration duration          [client mode] measurement period (default 10s)
      --enable-pprof               [client mode] enable pprof profiling
      --flavor string              [client mode] connect behavior type 'persistent' or 'ephemeral' (default "persistent")
      --interval duration          [client mode] interval for printing stats (default 5s)
      --jsonlines                  [client mode] output results in JSON Lines format
      --listen-addrs-file string   [server mode] enable to pass a file including a pair of addresses and ports
      --merge-results-each-host    [client mode] merge results of each host (with --show-only-results)
      --message-bytes int32        [client mode] TCP/UDP message size (bytes) (default 64)
      --pprof-addr string          [client mode] pprof listening address:port (default "localhost:6060")
      --proto string               [client mode] protocol (tcp or udp) (default "tcp")
      --protocol string            [server mode] listening protocol ('tcp' or 'udp') (default "all")
      --rate int32                 [client mode] New connections throughput (/s) (only for 'ephemeral') (default 100)
  -s, --server                     run in server mode
      --show-only-results          [client mode] print only results of measurement stats
      --version                    show version information

Examples:
  tcpulse -s                          # Start server on default port 9100
  tcpulse -s 0.0.0.0:8080             # Start server on port 8080
  tcpulse -c localhost:9100           # Connect to server as client
  tcpulse -c --connections 50 host:port # Connect with 50 connections
```

## Examples

### Commands

Run as a server.

```shell-session
$ tcpulse -s 127.0.0.1:9100
```

Run as a client to put a load on the server.

```shell-session
$ tcpulse -c --flavor ephemeral --rate 1000 --duration 15s 127.0.0.1:9100
```

```shell-session
$ tcpulse -c --flavor persistent --connections 1000 --duration 15s 127.0.0.1:9100
```

Run as a UDP client.

```shell-session
$ tcpulse -c --proto udp --rate 1000 --duration 15s 127.0.0.1:9100
```

Output results in JSON Lines format for integration with monitoring tools.

```shell-session
$ tcpulse -c --jsonlines --rate 1000 --duration 10s 127.0.0.1:9100
{"peer":"127.0.0.1:9100","count":9998,"latency_max_us":2156,"latency_min_us":145,"latency_mean_us":234,"latency_90p_us":289,"latency_95p_us":321,"latency_99p_us":456,"rate_per_sec":999.8,"timestamp":"2025-01-07T10:30:00Z"}
```

### Reports

#### Standard Output Format

```shell-session
$ tcpulse -c --proto tcp --flavor ephemeral --rate 1000 --duration 15s 10.0.150.2:9200 10.0.150.2:9300
PEER                 CNT        LAT_MAX(µs)     LAT_MIN(µs)     LAT_MEAN(µs)    LAT_90p(µs)     LAT_95p(µs)     LAT_99p(µs)     RATE(/s)
10.0.150.2:9200      4996       4108            212             367             446             492             773             999.00
10.0.150.2:9300      4999       10294           219             389             435             470             1595            999.40
10.0.150.2:9200      4998       3884            209             369             448             489             950             999.40
10.0.150.2:9300      4998       3057            220             356             426             473             1030            999.40
10.0.150.2:9200      4802       22838           219             784             1030            2692            10264           0.00
10.0.150.2:9300      4808       18730           219             801             1033            2382            13412           0.00
--- A result during total execution time ---
10.0.150.2:9200      14799      13736           223             530             501             953             5989            996.00
10.0.150.2:9300      14809      18023           212             542             492             1110            5849            996.47
```

#### JSON Lines Output Format

```shell-session
$ tcpulse -c --jsonlines --proto tcp --flavor ephemeral --rate 1000 --duration 15s 10.0.150.2:9200 10.0.150.2:9300
{"peer":"10.0.150.2:9200","count":14799,"latency_max_us":13736,"latency_min_us":223,"latency_mean_us":530,"latency_90p_us":501,"latency_95p_us":953,"latency_99p_us":5989,"rate_per_sec":996.0,"timestamp":"2025-01-07T10:30:00Z"}
{"peer":"10.0.150.2:9300","count":14809,"latency_max_us":18023,"latency_min_us":212,"latency_mean_us":542,"latency_90p_us":492,"latency_95p_us":1110,"latency_99p_us":5849,"rate_per_sec":996.47,"timestamp":"2025-01-07T10:30:00Z"}
```

The JSON Lines format includes the following fields:
- `peer`: Target server address
- `count`: Total number of successful connections/requests
- `latency_max_us`: Maximum latency in microseconds
- `latency_min_us`: Minimum latency in microseconds  
- `latency_mean_us`: Mean latency in microseconds
- `latency_90p_us`: 90th percentile latency in microseconds
- `latency_95p_us`: 95th percentile latency in microseconds
- `latency_99p_us`: 99th percentile latency in microseconds
- `rate_per_sec`: Average request rate per second
- `timestamp`: ISO 8601 timestamp when the measurement was completed

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
