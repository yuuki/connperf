# connperf

connperf is a load generator for measuring the performance of TCP/UDP connections in Go.

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
$ connperf connect --proto tcp --flavor ephemeral --rate 1000 --duration 15s 10.0.150.2:9200 10.0.150.2:9300
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
