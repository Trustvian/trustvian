MODULE   := github.com/Trustvian/trustvian
BINARY   := trustvian
BIN_DIR  := bin
CMD      := ./cmd/trustvian
GO       := go

.DEFAULT_GOAL := help

.PHONY: help build run demo baseline-demo test test-race bench vet fmt fmt-check tidy coverage install clean check examples

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the trustvian CLI to bin/trustvian
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(BINARY) $(CMD)

run: build ## Build and run the CLI, e.g. make run ARGS="analyze events.json"
	./$(BIN_DIR)/$(BINARY) $(ARGS)

demo: build ## Run analyze against the bundled example fixture
	./$(BIN_DIR)/$(BINARY) analyze cmd/trustvian/testdata/normal.json

baseline-demo: build ## Run baseline build against the bundled example corpus
	./$(BIN_DIR)/$(BINARY) baseline build cmd/trustvian/testdata/corpus.json

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run all tests with the race detector
	$(GO) test -race ./...

bench: ## Run all benchmarks with allocation stats
	$(GO) test -run '^$$' -bench . -benchmem ./...

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format all Go files in place
	gofmt -w .

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

coverage: ## Run tests with coverage and write an HTML report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

install: ## Install the CLI to GOBIN (or GOPATH/bin)
	$(GO) install $(CMD)

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

check: fmt-check vet build test-race ## Full local gate: fmt-check, vet, build, race tests

examples: ## Run every examples/* program and fail if any exits non-zero
	@for d in examples/*/; do \
		if [ -f "$$d/main.go" ]; then \
			echo "==> $$d"; \
			(cd "$$d" && go run .) || exit 1; \
		fi; \
	done
