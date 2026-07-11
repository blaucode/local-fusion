# local-fusion v2 — single entry point for build, test, docker, and checks.
# Run `make` or `make help` to see all commands.

# ─── config ──────────────────────────────────────────────────────────────────
BINARY  := local-fusion
IMAGE   := local-fusion
TAG     := 2
PORT    := 8484
VOLUME  := lf-data
ENVFILE := providers.env

# ─── containerized toolchain (rule: ALL commands and tools run in containers) ─
# Nothing executes on the host except docker and make themselves. Go runs in
# $(GO_IMAGE), scripts run in $(PY_IMAGE). Named volumes cache Go modules/builds.
GO_IMAGE    := golang:1.23
PY_IMAGE    := python:3.12-slim
GOCACHE_VOL := lf-gocache
RUN_GO      := docker run --rm -v $(CURDIR):/src -w /src \
               -v $(GOCACHE_VOL):/root/.cache -v $(GOCACHE_VOL)-mod:/go/pkg/mod \
               -e GOFLAGS=-buildvcs=false $(GO_IMAGE)
RUN_PY      := docker run --rm -v $(CURDIR):/src -w /src $(PY_IMAGE)

# v1 reference checkout for the prompt-freeze layer-2 check; mounted read-only
# into the container when present, skipped gracefully when not (e.g. CI).
V1_DIR      ?= ../../vendo/local-fusion
ifneq ($(wildcard $(V1_DIR)/orchestrator/fusion),)
V1_MOUNT    := -v $(abspath $(V1_DIR)):/v1:ro -e V1_DIR=/v1
else
V1_MOUNT    := -e V1_DIR=/v1-not-mounted
endif
RUN_PY_V1   := docker run --rm -v $(CURDIR):/src -w /src $(V1_MOUNT) $(PY_IMAGE)

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
.PHONY: help build test race lint check soak docker-build docker-run docker-stop docker-logs replay prompts-check docs-check clean spike-s1 spike-s2 spike-s3

# ─── meta ────────────────────────────────────────────────────────────────────
help: ## 📖 Show this help
	@printf "\n$(BOLD)$(CYAN)  local-fusion v2$(RESET) $(DIM)— make targets$(RESET)\n\n"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  $(CYAN)%-15s$(RESET) %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf "\n$(DIM)  ALL commands run in containers ($(GO_IMAGE), $(PY_IMAGE)) — host needs only docker + make.$(RESET)\n"
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
	@printf "$(CYAN)🔒 Checking prompt freeze (in $(PY_IMAGE))...$(RESET)\n"
	@$(RUN_PY_V1) bash scripts/prompts-diff.sh
	$(call ok,"prompts frozen")

prompts-extract: ## 🧊 Re-run the verbatim extraction from v1 (only after a reviewed v1 prompt change)
	@printf "$(CYAN)🧊 Extracting prompts from v1 (in $(PY_IMAGE))...$(RESET)\n"
	@$(RUN_PY_V1) sh -c 'python3 scripts/extract-prompts.py --v1 "$$V1_DIR"'
	$(call ok,"prompts/ regenerated — commit as a prompt-only change (ADR-008)")

docs-check: ## 📚 Verify all markdown links resolve (docs/ + product-docs/)
	@printf "$(CYAN)📚 Checking doc links (in $(PY_IMAGE))...$(RESET)\n"
	@$(RUN_PY) python3 scripts/check-links.py
	$(call ok,"links OK")

# ─── M1 spikes (own module in spikes/; SDK needs Go >= 1.25) ─────────────────
SPIKE_GO_IMAGE := golang:1.25
RUN_GO_SPIKE   := docker run --rm -v $(CURDIR):/src -w /src/spikes \
                  -v $(GOCACHE_VOL):/root/.cache -v $(GOCACHE_VOL)-mod:/go/pkg/mod \
                  -e GOFLAGS=-buildvcs=false

spike-s1: ## 🕵️ S1: net/http vs Featherless/Ollama (uses providers.env keys when present)
	@if [ -f $(ENVFILE) ]; then \
		$(RUN_GO_SPIKE) --env-file $(ENVFILE) $(SPIKE_GO_IMAGE) go run ./s1-providers; \
	else \
		printf "$(YELLOW)⚠ no providers.env — running unauthenticated edge probe only$(RESET)\n"; \
		$(RUN_GO_SPIKE) $(SPIKE_GO_IMAGE) go run ./s1-providers; \
	fi

spike-s2: ## 📡 S2: serve lf_echo over Streamable HTTP on 127.0.0.1:8484 (Ctrl-C to stop)
	@$(RUN_GO_SPIKE) -p 127.0.0.1:8484:8484 $(SPIKE_GO_IMAGE) go run ./s2-echo -addr :8484

spike-s3: ## ⏱️ S3: budget kill-switch prototype tests (-race)
	@$(RUN_GO_SPIKE) $(SPIKE_GO_IMAGE) go test -race -v ./s3-killswitch/

# ─── housekeeping ────────────────────────────────────────────────────────────
clean: ## 🧹 Remove build artifacts
	@printf "$(YELLOW)🧹 Cleaning...$(RESET)\n"
	@rm -rf bin/
	$(call ok,"clean")
