.PHONY: build test docker/build

OUT_BIN := connperf
GO := $(shell which go)
GO_SRC := $(shell find . -type f -name '*.go')
CMD_DOCKER ?= docker
OUT_DOCKER ?= connperf

all: build

build: $(OUT_BIN)

$(OUT_BIN): $(filter-out *_test.go,$(GO_SRC))
	go build -o $(OUT_BIN)

test:
	$(GO) test -v ./...

docker/build:
	$(CMD_DOCKER) build -t $(OUT_DOCKER):latest .
