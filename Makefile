# local-fusion v2 — single entry point for build, test, docker, and checks.
# Run `make` or `make help` to see all commands.

# ─── config ──────────────────────────────────────────────────────────────────
BINARY  := local-fusion
IMAGE   := local-fusion
TAG     := 2
PORT    := 8484
VOLUME  := lf-data
ENVFILE := providers.env

# ─── containerized toolchain (rule: never install toolchains on the host) ────
# All Go commands run inside $(GO_IMAGE); the only host requirements are
# docker, make, and python3. A named volume caches modules/builds across runs.
GO_IMAGE    := golang:1.23
GOCACHE_VOL := lf-gocache
RUN_GO      := docker run --rm -v $(CURDIR):/src -w /src \
               -v $(GOCACHE_VOL):/root/.cache -v $(GOCACHE_VOL)-mod:/go/pkg/mod \
               -e GOFLAGS=-buildvcs=false $(GO_IMAGE)

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
	@printf "\n$(DIM)  Toolchain runs in Docker ($(GO_IMAGE)) — host needs only docker, make, python3.$(RESET)\n"
	@printf "$(DIM)  Docs: docs/ (users) · product-docs/ (implementers) · AGENTS.md (agents)$(RESET)\n\n"

# ─── build & test (all inside the golang container) ─────────────────────────
build: ## 🔨 Build the Go binary (in Docker)
	@printf "$(CYAN)🔨 Building $(BINARY) in $(GO_IMAGE)...$(RESET)\n"
	@$(RUN_GO) go build -o bin/$(BINARY) ./cmd/$(BINARY)
	$(call ok,"built bin/$(BINARY) (linux)")

test: ## 🧪 Run all tests with -race (in Docker)
	@printf "$(CYAN)🧪 Running tests (-race) in $(GO_IMAGE)...$(RESET)\n"
	@$(RUN_GO) go test -race ./...
	$(call ok,"tests passed")

race: test ## 🏁 Alias for test (all tests always run under -race)

lint: ## 🔍 go vet + gofmt check (in Docker)
	@printf "$(CYAN)🔍 Linting in $(GO_IMAGE)...$(RESET)\n"
	@$(RUN_GO) go vet ./...
	@$(RUN_GO) sh -c 'out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi'
	$(call ok,"lint clean")

soak: ## 🌊 Job-runner soak test (M2 exit gate: concurrent jobs, cancellation storms)
	@printf "$(CYAN)🌊 Soak test (this takes a while)...$(RESET)\n"
	@$(RUN_GO) go test -race -tags=soak -timeout 30m ./internal/jobs/...
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
	@$(RUN_GO) go test -tags=replay ./internal/engine/... -run TestParity
	$(call ok,"parity holds")

prompts-check: ## 🔒 Verify prompts/*.tmpl are byte-identical to the v1 extraction (ADR-008)
	@printf "$(CYAN)🔒 Checking prompt freeze...$(RESET)\n"
	@if [ -x scripts/prompts-diff.sh ]; then \
		./scripts/prompts-diff.sh && printf "$(GREEN)✅ prompts unchanged$(RESET)\n"; \
	else \
		printf "$(YELLOW)⏳ M0 pending — scripts/prompts-diff.sh not created yet (see PROJECT-PLAN M0)$(RESET)\n"; \
	fi

docs-check: ## 📚 Verify all markdown links resolve (docs/ + product-docs/)
	@printf "$(CYAN)📚 Checking doc links...$(RESET)\n"
	@python3 scripts/check-links.py
	$(call ok,"links OK")

# ─── housekeeping ────────────────────────────────────────────────────────────
clean: ## 🧹 Remove build artifacts
	@printf "$(YELLOW)🧹 Cleaning...$(RESET)\n"
	@rm -rf bin/
	$(call ok,"clean")
