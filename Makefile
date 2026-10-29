BINARY      := note
PKG         := ./cmd/note
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2

.PHONY: build test lint install cross clean

## build: static binary in bin/
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

## test: all tests with the race detector
test:
	go test -race ./...

## lint: gofmt, go vet, staticcheck
lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	go vet ./...
	go run $(STATICCHECK) ./...

## install: install the binary into $(go env GOBIN) or $(go env GOPATH)/bin
install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

## cross: check that every release target builds
cross:
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
		echo "build $$target"; \
		CGO_ENABLED=0 GOOS=$${target%/*} GOARCH=$${target#*/} go build -trimpath -o /dev/null $(PKG) || exit 1; \
	done

clean:
	rm -rf bin
