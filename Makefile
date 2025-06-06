OUT_BIN := connperf
GO := $(shell which go)
GO_SRC := $(shell find . -type f -name '*.go')
CMD_DOCKER ?= docker
OUT_DOCKER ?= connperf

all: build

build: $(OUT_BIN)

$(OUT_BIN): $(filter-out *_test.go,$(GO_SRC))
	go build -o $(OUT_BIN)

.PHONY: docker test
docker:
	$(CMD_DOCKER) build -t $(OUT_DOCKER):latest .

test:
	$(GO) test ./...
