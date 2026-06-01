# ChronoCascade Memory Engine — local task runner.
#
# Targets are grouped: build / run / test / inspect / clean / quality.
# `make help` prints a summary. All targets are .PHONY by design — this is a
# task runner, not a build graph.

SHELL          := /usr/bin/env bash
.DEFAULT_GOAL  := help

# Directories
BIN_DIR        := bin
DEMO_DIR       ?= ./memory
GO_PKGS        := ./...

# Binaries
CCME_BIN       := $(BIN_DIR)/ccme

# Go toolchain knobs
GOFLAGS        ?=
GO_TEST_FLAGS  ?= -count=1

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} \
	     /^[a-zA-Z0-9_.-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' \
	     $(MAKEFILE_LIST) | sort
	@echo ""
	@echo "  Examples:"
	@echo "    make demo                # reset store, run end-to-end demo"
	@echo "    make inspect DEMO_DIR=./memory"
	@echo "    DEMO_DIR=/tmp/ccme make run"

# ── build ────────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Compile the ccme CLI into bin/ccme.
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -o $(CCME_BIN) ./cmd/ccme
	@echo "built $(CCME_BIN)"

.PHONY: tidy
tidy: ## Run go mod tidy.
	go mod tidy

# ── run ──────────────────────────────────────────────────────────────────────

.PHONY: run
run: ## Run the demo against $(DEMO_DIR) (default ./memory).
	go run ./cmd/ccme -dir $(DEMO_DIR)

.PHONY: demo
demo: ## Reset $(DEMO_DIR) and run the full demo (idempotent fresh start).
	go run ./cmd/ccme -reset -dir $(DEMO_DIR)

# ── test ─────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all unit tests.
	go test $(GO_TEST_FLAGS) $(GO_PKGS)

.PHONY: test-v
test-v: ## Run all unit tests with verbose output.
	go test -v $(GO_TEST_FLAGS) $(GO_PKGS)

.PHONY: cover
cover: ## Run tests with coverage; writes coverage.out.
	go test -coverprofile=coverage.out $(GO_TEST_FLAGS) $(GO_PKGS)
	@echo ""
	@go tool cover -func=coverage.out | tail -10

# ── inspect ──────────────────────────────────────────────────────────────────

.PHONY: inspect
inspect: ## List the on-disk layout of $(DEMO_DIR).
	@if [ ! -d "$(DEMO_DIR)" ]; then \
		echo "(no store at $(DEMO_DIR) — run 'make demo' first)"; \
		exit 0; \
	fi
	@echo "== $(DEMO_DIR) =="
	@find $(DEMO_DIR) -maxdepth 4 -type f | sort

.PHONY: inspect-sql
inspect-sql: ## Dump the SQLite tables and row counts (requires sqlite3 in PATH).
	@if ! command -v sqlite3 >/dev/null 2>&1; then \
		echo "sqlite3 not found in PATH"; exit 1; \
	fi
	@if [ ! -f "$(DEMO_DIR)/index.db" ]; then \
		echo "(no index.db at $(DEMO_DIR) — run 'make demo' first)"; \
		exit 0; \
	fi
	@for t in events tags associations schemas user_profiles \
	          session_contexts session_summaries chat_messages; do \
		n=$$(sqlite3 $(DEMO_DIR)/index.db "SELECT COUNT(*) FROM $$t;" 2>/dev/null || echo 0); \
		printf "  %-20s %5s rows\n" "$$t" "$$n"; \
	done

.PHONY: peek
peek: ## Print the first markdown event under l0/ (handy spot-check).
	@f=$$(find $(DEMO_DIR)/l0 -name '*.md' | head -1 2>/dev/null); \
	if [ -z "$$f" ]; then \
		echo "(no l0/*.md files under $(DEMO_DIR))"; exit 0; \
	fi; \
	echo "== $$f =="; \
	head -40 "$$f"

# ── quality ──────────────────────────────────────────────────────────────────

.PHONY: vet
vet: ## go vet across the module.
	go vet $(GO_PKGS)

.PHONY: fmt
fmt: ## gofmt -w on the whole tree.
	gofmt -w .

.PHONY: check
check: fmt vet test ## fmt + vet + test.

# ── clean ────────────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf $(BIN_DIR) coverage.out

.PHONY: clean-store
clean-store: ## Remove $(DEMO_DIR) entirely. Refuses to touch paths above the repo.
	@case "$(DEMO_DIR)" in \
		"" | "/" | "/*") echo "refusing to remove '$(DEMO_DIR)'"; exit 1 ;; \
	esac
	rm -rf $(DEMO_DIR)
	@echo "removed $(DEMO_DIR)"

.PHONY: clean-all
clean-all: clean clean-store ## clean + remove the on-disk store.
