VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse HEAD)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build install test test-race lint fmt setup-hooks check smoke-live clean

build:
	go build -ldflags '$(LDFLAGS)' -o agent-deck .

install:
	go install -ldflags '$(LDFLAGS)' .

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	golangci-lint config verify
	golangci-lint run ./...

fmt:
	golangci-lint fmt

setup-hooks:
	@command -v lefthook >/dev/null || { echo 'install lefthook first'; exit 1; }
	lefthook install

check:
	golangci-lint config verify
	golangci-lint fmt --diff
	golangci-lint run ./...
	go test ./...

smoke-live:
	go test -tags live ./internal/opencode -run TestLive -v

clean:
	rm -f agent-deck
	rm -rf dist
