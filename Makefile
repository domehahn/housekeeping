BINARY     := scm-cleaner
MODULE     := github.com/domehahn/housekeeping
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOVERSION  := $(shell go version | awk '{print $$3}')

LDFLAGS := -X '$(MODULE)/pkg/version.Version=$(VERSION)' \
           -X '$(MODULE)/pkg/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/pkg/version.BuildDate=$(BUILD_DATE)' \
           -X '$(MODULE)/pkg/version.GoVersion=$(GOVERSION)'

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: build-all
build-all:
	@for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		ext=$$( [ "$${platform%/*}" = "windows" ] && echo ".exe" || echo "" ); \
		out=bin/$(BINARY)-$${platform%/*}-$${platform#*/}$$ext; \
		echo "building $$out"; \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} go build -ldflags "$(LDFLAGS)" -o $$out ./cmd/$(BINARY) || exit 1; \
	done

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	go tool cover -func=coverage.out | tail -1

.PHONY: integration-test
integration-test:
	GITLAB_INTEGRATION_TEST=true go test ./test/integration/...

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed - see https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run

.PHONY: fmt
fmt:
	gofmt -l -w .
	go run golang.org/x/tools/cmd/goimports@latest -l -w . 2>/dev/null || true

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -rf bin/ dist/ coverage.out coverage.html

.PHONY: check
check: fmt vet test lint
