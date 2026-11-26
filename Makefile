BIN := bin/ship
SHELL := /bin/sh

# make build stamps the version with git describe; release builds pass VERSION.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
ifeq ($(strip $(VERSION)),)
VERSION := dev
endif
LDFLAGS := -X github.com/harrisonclewis/ship/internal/version.Version=$(VERSION)

.PHONY: build install dist test fmt lint check clean

build:
	mkdir -p bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) .

install:
	go install -trimpath -ldflags '$(LDFLAGS)' .

# Cross-compile the six release assets. VERSION should be set by the caller.
dist:
	rm -rf dist
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-linux-x64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-linux-arm64 .
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-darwin-x64 .
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-windows-x64.exe .
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/ship-windows-arm64.exe .

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	go vet ./...

# Everything CI should run. shellcheck is used when installed.
check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	sh -n install.sh
	bash -n install.sh
	bash -n scripts/release-keygen.sh
	bash -n ship-agent
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck ship-agent install.sh scripts/release-keygen.sh; \
	else \
		echo "shellcheck not installed; bash -n only"; \
	fi

clean:
	rm -rf bin dist
