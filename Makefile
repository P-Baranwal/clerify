BINARY  := clerify
MODULE  := github.com/P-Baranwal/clerify
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: all build test vet fmt install clean check

all: check build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/clerify

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

check: vet test
	@unformatted=$$(gofmt -l ./cmd ./internal); \
	if [ -n "$$unformatted" ]; then echo "unformatted files:"; echo "$$unformatted"; exit 1; fi

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/clerify

clean:
	rm -rf bin dist
