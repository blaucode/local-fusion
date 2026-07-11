# local-fusion v2 — single entry point for build, test, docker, and checks.
# Run `make` or `make help` to see all commands.

# ─── config ──────────────────────────────────────────────────────────────────
BINARY  := local-fusion
IMAGE   := local-fusion
TAG     := 2
PORT    := 8484
VOLUME  := lf-data
ENVFILE := providers.env

# ─── colors & icons ──────────────────────────────────────────────────────────
BOLD   := \033[1m
CYAN   := \033[36m
GREEN  := \033[32m
YELLOW := \033[33m
RED    := \033[31m
DIM    := \033[2m
RESET  := \033[0m

define ok
	@printf "$(GREEN)✅ %s$(RESET)\n" $(1)
endef

.DEFAULT_GOAL := help
.PHONY: help build test race lint check soak docker-build docker-run docker-stop docker-logs replay prompts-check docs-check clean

# ─── meta ────────────────────────────────────────────────────────────────────
help: ## 📖 Show this help
	@printf "\n$(BOLD)$(CYAN)  local-fusion v2$(RESET) $(DIM)— make targets$(RESET)\n\n"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  $(CYAN)%-15s$(RESET) %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf "\n$(DIM)  Docs: docs/ (users) · product-docs/ (implementers) · AGENTS.md (agents)$(RESET)\n\n"

# ─── build & test ────────────────────────────────────────────────────────────
build: ## 🔨 Build the Go binary
	@printf "$(CYAN)🔨 Building $(BINARY)...$(RESET)\n"
	@go build -o bin/$(BINARY) ./cmd/$(BINARY)
	$(call ok,"built bin/$(BINARY)")

test: ## 🧪 Run all tests (with -race)
	@printf "$(CYAN)🧪 Running tests (-race)...$(RESET)\n"
	@go test -race ./...
	$(call ok,"tests passed")

race: test ## 🏁 Alias for test (all tests always run under -race)

lint: ## 🔍 go vet + gofmt check
	@printf "$(CYAN)🔍 Linting...$(RESET)\n"
	@go vet ./...
	@out=$$(gofmt -l . 2>/dev/null); if [ -n "$$out" ]; then \
		printf "$(RED)❌ gofmt needed:$(RESET)\n$$out\n"; exit 1; fi
	$(call ok,"lint clean")

soak: ## 🌊 Job-runner soak test (M2 exit gate: concurrent jobs, cancellation storms)
	@printf "$(CYAN)🌊 Soak test (this takes a while)...$(RESET)\n"
	@go test -race -tags=soak -timeout 30m ./internal/jobs/...
	$(call ok,"soak passed — no leaks, no races")

check: lint test prompts-check docs-check ## ✅ Everything CI runs, locally
	$(call ok,"all checks passed")

# ─── docker ──────────────────────────────────────────────────────────────────
docker-build: ## 🐳 Build the Docker image
	@printf "$(CYAN)🐳 Building $(IMAGE):$(TAG)...$(RESET)\n"
	@docker build -t $(IMAGE):$(TAG) .
	$(call ok,"image $(IMAGE):$(TAG) built")

docker-run: ## 🚀 Run the server container (HTTP :8484, volume, env-file)
	@if [ ! -f $(ENVFILE) ]; then \
		printf "$(RED)❌ $(ENVFILE) not found.$(RESET) $(DIM)See docs/configuration.md — copy providers.env.example and add your keys.$(RESET)\n"; exit 1; fi
	@printf "$(CYAN)🚀 Starting $(IMAGE):$(TAG) on 127.0.0.1:$(PORT)...$(RESET)\n"
	@docker run -d --name $(BINARY) -p 127.0.0.1:$(PORT):$(PORT) \
		-v $(VOLUME):/data --env-file $(ENVFILE) $(IMAGE):$(TAG)
	$(call ok,"server up → http://localhost:$(PORT)/mcp  (healthz: /healthz)")

docker-stop: ## 🛑 Stop and remove the server container
	@printf "$(YELLOW)🛑 Stopping $(BINARY)...$(RESET)\n"
	@docker rm -f $(BINARY) >/dev/null 2>&1 || true
	$(call ok,"stopped")

docker-logs: ## 📜 Tail server logs
	@docker logs -f $(BINARY)

# ─── parity & guardrails ─────────────────────────────────────────────────────
replay: ## 📼 Deterministic parity: replay recorded v1 requests against the Go engine (ADR-010)
	@printf "$(CYAN)📼 Record/replay parity...$(RESET)\n"
	@go test -tags=replay ./internal/engine/... -run TestParity
	$(call ok,"parity holds")

prompts-check: ## 🔒 Verify prompts/*.tmpl are byte-identical to the v1 extraction (ADR-008)
	@printf "$(CYAN)🔒 Checking prompt freeze...$(RESET)\n"
	@./scripts/prompts-diff.sh
	$(call ok,"prompts unchanged")

docs-check: ## 📚 Verify all markdown links resolve (docs/ + product-docs/)
	@printf "$(CYAN)📚 Checking doc links...$(RESET)\n"
	@python3 scripts/check-links.py
	$(call ok,"links OK")

# ─── housekeeping ────────────────────────────────────────────────────────────
clean: ## 🧹 Remove build artifacts
	@printf "$(YELLOW)🧹 Cleaning...$(RESET)\n"
	@rm -rf bin/
	$(call ok,"clean")
