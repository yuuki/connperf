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
$ connperf connect --flavor ephemeral --rate 1000 --duration 15s 127.0.0.1:9100
```

```shell
$ connperf connect --flavor persistent --connections 1000 --duration 15s 127.0.0.1:9100
```

Run as a UDP client.

```shell
$ connperf connect --proto udp --rate 1000 --duration 15s 127.0.0.1:9100
```

### Reports

```shell
$ connperf connect --proto tcp --flavor ephemeral --rate 1000 --duration 30s 127.0.0.1:9100
CNT        LAT_MAX(µs)     LAT_MIN(µs)     LAT_MEAN(µs)    LAT_90p(µs)     LAT_95p(µs)     LAT_99p(µs)     RATE(/s)
5000       1931            257             396             484             510             607             1000.00
4998       2756            270             416             493             533             805             999.40
4998       8665            276             412             479             517             741             999.40
4999       4017            255             394             460             490             620             999.60
4998       3595            263             396             459             501             791             999.60
--- A result during total execution time ---
29996      2827            267             401             477             509             675             999.95
```
