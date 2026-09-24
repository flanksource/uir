OS   = $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH = $(shell uname -m | sed 's/x86_64/amd64/')

LOCALBIN ?= $(shell pwd)/.bin
export PATH := $(LOCALBIN):$(PATH)
GOWORK ?= off
export GOWORK

GOLANGCI_LINT_VERSION ?= v2.6.2
VERSION ?= dev

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_:-]+:.*## /{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: web-build binary ## Build the browser, compile every package, and link the UIR CLI
	go build ./...

.PHONY: binary
binary: | $(LOCALBIN) ## Link the UIR CLI into .bin/uir (requires built web/dist)
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(LOCALBIN)/uir ./cmd/uir

.PHONY: install
install: web-build ## Install the UIR CLI with embedded browser assets
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/uir

.PHONY: web-build
web-build: ## Build embedded browser assets
	VITE_UIR_VERSION=$(VERSION) pnpm --dir web run build

.PHONY: schema
schema: ## Regenerate schema/uir.schema.json from the Go model
	go run ./cmd/genschema

.PHONY: query-parser
query-parser: ## Regenerate the PEG query parser
	go generate ./query

.PHONY: test
test: ## Run Go and browser tests
	go test ./...
	pnpm --dir web run test
	pnpm --dir packages/api run test

.PHONY: test-python
test-python: ## Run the Python UIR model test
	python3 python/test_uir.py

.PHONY: fmt
fmt: ## Format and tidy
	go fmt ./...
	go mod tidy

.PHONY: wasm-check
wasm-check: ## Compile the model package for js/wasm (browser-side consumers)
	GOOS=js GOARCH=wasm go build -o /dev/null .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

$(LOCALBIN)/golangci-lint: | $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: $(LOCALBIN)/golangci-lint ## Run Go and browser lint
	$(LOCALBIN)/golangci-lint run
	pnpm --dir web run lint

.PHONY: clean
clean: ## Remove build and scratch output
	rm -rf $(LOCALBIN) .tmp coverage.out coverage.html
