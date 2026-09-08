.DEFAULT_GOAL := help

GO ?= go

.PHONY: help
help: ## List the available commands
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile every package
	$(GO) build ./...

.PHONY: test
test: ## Run the tests with the race detector
	$(GO) test -race ./...

.PHONY: test-short
test-short: ## Run only the fast tests (skip the ones needing containers)
	$(GO) test -short ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: fmt
fmt: ## Format the source
	$(GO) fmt ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: proto
proto: ## Regenerate Go code from the .proto files (needs buf)
	cd proto && buf generate
