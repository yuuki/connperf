FROM golang:1.15 as builder

FROM builder as build
WORKDIR /connperf
COPY . /connperf
RUN make build CGO_ENABLED=0

FROM alpine as runtime
COPY --from=build /connperf/connperf ./
ENTRYPOINT ["./connperf"]
