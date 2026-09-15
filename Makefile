GO_IMAGE := golang:1.25.10
ROOT_DIR := $(CURDIR)
LABEL ?= baseline

.PHONY: help templ test bench lint tui-golden dev-up dev-down dev-seed dev-smoke dev-gate

help: ## List the available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

templ: ## Regenerate the templ-generated Go files for the cloud dashboard
	go tool templ generate ./internal/cloud/dashboard/...

test: ## Run the full suite (unit + e2e + TUI coverage gate) in the pinned container
	bash scripts/dev/test.sh

bench: ## Run the benchmark suite in the pinned container (LABEL defaults to baseline)
	bash scripts/dev/bench.sh $(LABEL)

lint: ## gofmt -l and go vet, both in the pinned container
	docker run --rm \
		-v "$(ROOT_DIR):/src" \
		-v engram-dev-gomod:/go/pkg/mod \
		-v engram-dev-gobuild:/root/.cache/go-build \
		-w /src $(GO_IMAGE) \
		sh -c 'out=$$(gofmt -l ./cmd ./internal ./plugin); if [ -n "$$out" ]; then echo "$$out"; echo "gofmt -l found unformatted files" >&2; exit 1; fi; go vet ./...'

tui-golden: ## Regenerate the TUI golden files (explicit approval only)
	bash scripts/dev/golden.sh --approve

dev-up: ## Build and start the isolated dev container
	bash scripts/dev/up.sh

dev-down: ## Stop the dev environment and remove its volumes
	bash scripts/dev/down.sh

dev-seed: ## Fill the dev store with the fixture workspace
	bash scripts/dev/seed.sh

dev-smoke: ## Run the MCP smoke suite against the dev container
	bash scripts/dev/mcp-smoke.sh

dev-gate: dev-up dev-seed dev-smoke dev-down ## Full local gate: up, seed, smoke, down
