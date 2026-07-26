VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fmt install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/acn ./cmd/acn

test:
	go vet ./...
	go test -race ./...

fmt:
	gofmt -w .

# 装到 GOBIN，便于不经 Homebrew 直接试用
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/acn

clean:
	rm -rf bin
