# connperf

connperf is a measturement tool for TCP/UDP connections in Go.

## Examples

### Commands

Run as a server.

```shell
connperf serve -l 127.0.0.1:9100
```

Run as a client to put a load on the server.

```shell
connperf connect --type ephemeral --rate 1000 --duration 15s 127.0.0.1:9100
```

```shell
connperf connect --type persistent --connections 1000 --duration 15s 127.0.0.1:9100
```

Run as a UDP client.

```shell
connperf connect --proto udp --rate 1000 --duration 15s 127.0.0.1:9100
```

### Reports

```shell
connperf connect --proto udp --rate 1000 --duration 15s 127.0.0.1:9100
--- Execution results of connperf ---
Total number of connections: 4996
Connect latency (max): 6142 µs
Connect latency (min): 272 µs
Connect latency (mean): 418 µs
Connect latency (90p): 479 µs
Connect latency (95p): 518 µs
Connect latency (99p): 1104 µs
Rate: 999.00 /s
```
