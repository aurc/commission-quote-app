# Commission Quote App
#
# Services are built from cmd/<name>. Targets appear as they land, per the task
# register in tasks/register.md.

GO      ?= go
SERVICES = cqapi middleware bff
BIN      = bin

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build every service into bin/
	@mkdir -p $(BIN)
	@for s in $(SERVICES); do \
		if [ -d cmd/$$s ]; then \
			echo "building $$s"; \
			$(GO) build -o $(BIN)/$$s ./cmd/$$s || exit 1; \
		fi; \
	done

.PHONY: test
test: ## Run tests with the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Report test coverage per package
	$(GO) test -cover ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: vet ## Run go vet, then golangci-lint when installed
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, ran go vet only"; \
		echo "install: https://golangci-lint.run/welcome/install/"; \
	fi

.PHONY: fmt
fmt: ## Format the tree
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## Tidy module requirements
	$(GO) mod tidy

.PHONY: check
check: fmt vet test ## Format, vet and test. Run before opening a PR

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN)

# Native dev. The reviewer path is docker compose, added in CQ-08.
.PHONY: run-cqapi run-middleware run-bff
run-cqapi: ## Run the mocked vendor API
	$(GO) run ./cmd/cqapi
run-middleware: ## Run the middleware
	$(GO) run ./cmd/middleware
run-bff: ## Run the BFF
	$(GO) run ./cmd/bff
