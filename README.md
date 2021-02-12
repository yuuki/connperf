# connperf

connperf is a measturement tool for TCP/UDP connections in Go.

## Examples

### Commands

Run as a server.

```shell
$ connperf serve -l 127.0.0.1:9100
```

Run as a client to put a load on the server.

```shell
$ connperf connect --type ephemeral --rate 1000 --duration 15s 127.0.0.1:9100
```

```shell
$ connperf connect --type persistent --connections 1000 --duration 15s 127.0.0.1:9100
```

Run as a UDP client.

```shell
$ connperf connect --proto udp --rate 1000 --duration 15s 127.0.0.1:9100
```

### Reports

```shell
$ connperf connect --proto tcp --type ephemeral --rate 1000 --duration 30s 127.0.0.1:9100
Trying to connect to "127.0.0.1:9100" with "ephemeral" connections (rate: 1000, duration: 30s)
CNT        LAT_MAX(µs)     LAT_MIN(µs)     LAT_MEAN(µs)    LAT_90p(µs)     LAT_95p(µs)     LAT_99p(µs)     RATE(/s)
4985       7277            274             432             496             546             1366            997.00
4999       4836            271             421             489             524             661             999.80
4997       2082            304             459             528             560             799             999.20
4998       4400            291             461             538             571             827             999.40
4997       7312            264             425             506             542             943             999.40
```
