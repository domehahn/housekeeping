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

.PHONY: fmt-check
fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt formatting needed:"; gofmt -l .; exit 1; }

.PHONY: tidy-check
tidy-check:
	go mod tidy
	@git diff --exit-code go.mod go.sum || { echo "go.mod or go.sum is not tidy"; exit 1; }

.PHONY: vuln-check
vuln-check:
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

.PHONY: checksums
checksums: build-all
	@echo "generating SHA-256 checksums..."
	@cd bin && shasum -a 256 * > SHA256SUMS 2>/dev/null || cd bin && sha256sum * > SHA256SUMS

.PHONY: sbom
sbom:
	@echo "generating SBOM..."
	@mkdir -p bin
	@command -v syft >/dev/null 2>&1 && syft dir:. -o spdx-json=bin/sbom.spdx.json || echo "syft not installed; skipping SBOM generation"

.PHONY: release
release: build-all checksums sbom
	@echo "=== Supply Chain Release Artifacts Ready ==="
	@echo "Binaries & Checksums: bin/"
	@echo "To sign release checksums with Cosign:"
	@echo "  cosign sign-blob --key cosign.key bin/SHA256SUMS"

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -rf bin/ dist/ coverage.out coverage.html

.PHONY: check
check: fmt-check tidy-check vet test test-race lint

