VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/stansat/proby/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

BIN := proby
ifeq ($(OS),Windows_NT)
	BIN := proby.exe
endif

.PHONY: build build-all test vet lint run docker-linux clean tidy

build: ## Build the binary for the host platform
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/proby

build-all: ## Cross-compile the full release matrix into dist/
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_linux_amd64      ./cmd/proby
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_linux_arm64      ./cmd/proby
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_windows_amd64.exe ./cmd/proby
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_windows_arm64.exe ./cmd/proby
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_darwin_amd64     ./cmd/proby
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/proby_darwin_arm64     ./cmd/proby

test: ## Run all unit tests
	go test ./...

vet: ## Run go vet for host, linux and windows
	go vet ./...
	GOOS=linux go vet ./...
	GOOS=windows go vet ./...

lint: ## Run golangci-lint if available
	golangci-lint run ./... || echo "golangci-lint not installed"

run: build ## Build and run with the example config
	./$(BIN) -c proby.example.yml run

docker-linux: ## Build the Linux Docker image
	docker build -t proby:latest .

tidy: ## Tidy go.mod
	go mod tidy

clean: ## Remove build artifacts
	rm -rf dist $(BIN) proby proby.exe proby.history
