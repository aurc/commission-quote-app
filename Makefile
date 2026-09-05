# Commission Quote App
#
# Services are built from cmd/<name>. Targets appear as they land, per the task
# register in tasks/register.md.

GO      ?= go
# Our components carry the cqapp- prefix. The vendor stand in is named for what
# it is, so nothing reads as production code by accident. It is the one binary
# deleted when the real vendor arrives.
SERVICES = cqapi-mock cqapp-middleware cqapp-bff

# Load .env into the run targets only, never globally. Exporting it for every
# target would leak a developer's environment into `make test`, and a test that
# passes or fails depending on what is in someone's .env is worse than no test.
# Docker compose sets its own environment and never reads this file.
RUN_ENV = set -a; if [ -f .env ]; then . ./.env; else echo "no .env found, run 'make env' first" >&2; fi; set +a;
BIN      = bin

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

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
test: ## Run Go tests with the race detector
	$(GO) test -race ./...

.PHONY: test-web
test-web: ## Run front end tests
	@cd web && npm test --silent

.PHONY: smoke
smoke: ## Run the Postman collection against a running stack
	@cd web && npx --yes newman run ../postman/commission-quote.postman_collection.json \
		-e ../postman/local.postman_environment.json --reporters cli

.PHONY: cover
cover: ## Report test coverage per package
	$(GO) test -cover ./...

# The gate is on internal/, where the logic is. cmd/ is wiring, and it is
# covered by the compose smoke test rather than by unit tests, so including it
# would only teach people to ignore the number.
GO_COVER_MIN ?= 80

.PHONY: cover-check
cover-check: ## Fail if Go coverage of internal/ falls below GO_COVER_MIN
	@$(GO) test -coverpkg=./internal/... -coverprofile=coverage.out ./internal/... > /dev/null
	@$(GO) tool cover -func=coverage.out | tail -1
	@total=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$NF}' | tr -d '%'); \
	  awk -v t="$$total" -v m="$(GO_COVER_MIN)" 'BEGIN { \
	    if (t+0 < m+0) { printf "coverage %.1f%% is below the %s%% minimum\n", t, m; exit 1 } \
	    printf "coverage %.1f%% meets the %s%% minimum\n", t, m }'

.PHONY: cover-web
cover-web: ## Front end coverage, thresholds enforced by vite.config.ts
	@cd web && npx vitest run --coverage

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
check: fmt vet test test-web ## Format, vet and test everything. Run before opening a PR

.PHONY: ci
ci: vet cover-check cover-web ## Everything CI runs, minus the containers

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN) web/dist web/coverage coverage.out

# Native dev. One target per cmd/ entry, named after it. The reviewer path is
# docker compose, added in CQ-08.
.PHONY: up
up: ## Run the whole stack in Docker on http://localhost:8080
	docker compose -f deploy/compose.yaml up --build

.PHONY: up-debug
up-debug: ## Run the stack with the internal services published, for the Postman collection
	docker compose -f deploy/compose.yaml -f deploy/compose.debug.yaml up --build

.PHONY: down
down: ## Stop the stack and remove its containers
	docker compose -f deploy/compose.yaml -f deploy/compose.debug.yaml down --remove-orphans

.PHONY: logs
logs: ## Follow the stack's logs
	docker compose -f deploy/compose.yaml logs -f

.PHONY: env
env: ## Create .env from .env.example if it does not exist
	@if [ -f .env ]; then \
		echo ".env already exists, leaving it alone"; \
	else \
		cp .env.example .env && echo "created .env from .env.example"; \
	fi

.PHONY: dev-staff
dev-staff: ## Add a staff member to the fixtures, prompting for a password
	@$(RUN_ENV) $(GO) run ./cmd/devstaff $(ARGS)

.PHONY: dev-token
dev-token: ## Print a bearer token, to call the middleware without the BFF
	@$(RUN_ENV) $(GO) run ./cmd/devtoken $(ARGS)

.PHONY: run-cqapi-mock run-cqapp-middleware run-cqapp-bff run-cqapp-web
run-cqapi-mock: ## Run the vendor mock, port 8083
	@$(RUN_ENV) $(GO) run ./cmd/cqapi-mock
run-cqapp-middleware: ## Run the middleware, port 8082
	@$(RUN_ENV) $(GO) run ./cmd/cqapp-middleware
run-cqapp-bff: ## Run the BFF, port 8081
	@$(RUN_ENV) $(GO) run ./cmd/cqapp-bff
run-cqapp-web: ## Run the front end, port 5173
	@cd web && npm run dev
