INSTALL_DIR ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/rstnk/igo/internal/cli.Version=$(VERSION)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build igo into bin/
	go build -ldflags "$(LDFLAGS)" -o bin/igo ./cmd/igo

.PHONY: run
run: ## Run igo (pass templates with ARGS, e.g. make run ARGS="go macos")
	go run ./cmd/igo $(ARGS)

.PHONY: vendor
vendor: ## Re-snapshot the embedded templates and index from github/gitignore
	go generate ./templates

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: lint
lint: ## Run go vet (and staticcheck when installed)
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

.PHONY: fmt
fmt: ## Format all Go files
	go fmt ./...

.PHONY: modernize
modernize: ## Apply go fix modernizations
	go fix ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: install
install: build ## Install igo into ~/.local/bin
	@mkdir -p $(INSTALL_DIR)
	install -m 0755 bin/igo $(INSTALL_DIR)/igo
	@echo "installed $(INSTALL_DIR)/igo"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/
