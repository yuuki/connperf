# connperf

connperf is a measturement tool for TCP connections in Go.

## Examples

```shell
connperf serve -p 9000
connperf connect --persistent-connections 10000 --rate 1000 <server addr>:9000
```
