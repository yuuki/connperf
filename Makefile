.PHONY: build test vet staticcheck revive docker/build

OUT_BIN := tcpulse
GO := $(shell which go)
CMD_DOCKER ?= docker
OUT_DOCKER ?= tcpulse

all: build

build: vet staticcheck
	CGO_ENABLED=0 $(GO) build -o $(OUT_BIN)

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) tool staticcheck ./...

revive:
	$(GO) tool revive ./...

test: vet staticcheck
	$(GO) test -race -v ./...

docker/build:
	$(CMD_DOCKER) build -t $(OUT_DOCKER):latest .
