# connperf

connperf is a measturement tool for TCP connections in Go.

## Examples

Run as a server.

```shell
connperf serve -l 127.0.0.1:9100
```

Give the server load as a client.

```shell
connperf connect --connections 1000 --rate 100 --duration 15s 12.0.0.1:9100
```
