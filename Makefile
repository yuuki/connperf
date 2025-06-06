.PHONY: build test docker/build

OUT_BIN := connperf
GO := $(shell which go)
CMD_DOCKER ?= docker
OUT_DOCKER ?= connperf

all: vet build

build: vet
	$(GO) build -o $(OUT_BIN)

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -v ./...

docker/build:
	$(CMD_DOCKER) build -t $(OUT_DOCKER):latest .
