.DEFAULT_GOAL := help

GO ?= go

.PHONY: help
help: ## Komutları listele
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Tüm paketleri derle
	$(GO) build ./...

.PHONY: test
test: ## Testleri çalıştır (yarış tespiti açık)
	$(GO) test -race ./...

.PHONY: test-short
test-short: ## Yalnızca hızlı testler (container gerektirenleri atla)
	$(GO) test -short ./...

.PHONY: lint
lint: ## golangci-lint çalıştır
	golangci-lint run

.PHONY: tidy
tidy: ## go.mod ve go.sum düzenle
	$(GO) mod tidy

.PHONY: fmt
fmt: ## Kaynak kodu biçimlendir
	$(GO) fmt ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...
