OS   = $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH = $(shell uname -m | sed 's/x86_64/amd64/')

LOCALBIN ?= $(shell pwd)/.bin
export PATH := $(LOCALBIN):$(PATH)

GOLANGCI_LINT_VERSION ?= v2.6.2

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_:-]+:.*## /{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Compile every package
	go build ./...

.PHONY: test
test: ## Run the Go test suite
	go test ./...

.PHONY: test-python
test-python: ## Run the Python UIR model test
	python3 python/test_uir.py

.PHONY: fmt
fmt: ## Format and tidy
	go fmt ./...
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

$(LOCALBIN)/golangci-lint: $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: $(LOCALBIN)/golangci-lint ## Run golangci-lint
	$(LOCALBIN)/golangci-lint run

.PHONY: clean
clean: ## Remove build and scratch output
	rm -rf $(LOCALBIN) .tmp coverage.out coverage.html
