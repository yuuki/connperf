# connperf

connperf is a measturement tool for TCP/UDP connections in Go.

## Examples

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

