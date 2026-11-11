VERSION ?= 0.1.0-dev
GO ?= $(if $(wildcard .tools/go/bin/go),.tools/go/bin/go,go)
GOFMT ?= $(if $(wildcard .tools/go/bin/gofmt),.tools/go/bin/gofmt,gofmt)

.PHONY: build check test lint fmt demo

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/launchproof ./cmd/launchproof

test:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...
	test -z "$$($(GOFMT) -l cmd internal)"

check: lint test

fmt:
	$(GOFMT) -w cmd internal

demo: build
	vhs docs/demo.tape
