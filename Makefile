VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test

build:
	go build $(LDFLAGS) -o godex ./cmd/godex

test:
	go test ./...
