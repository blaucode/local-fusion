# Project Plan & Engineering Process — local-fusion v2

**Owner:** Adolfo · **Date:** 2026-07-09 · **Companions:** [PRD.md](./PRD.md) · [adr/](./adr/)

Solo-builder plan with team checkpoints. Effort in focused evenings/weekends ("sessions",
~2–3h each); calendar estimates assume ~4 sessions/week. Every milestone has an exit gate —
**a milestone without a passing gate does not close.**

---

## Port contract — what must NOT change

The v1 Python engine (`vendo/local-fusion/orchestrator/fusion/`) is the reference
implementation. Invariant across the entire migration:

- `providers.yaml` schema (registry, pipelines, panels, roles) — a v1 file loads unmodified.
- **Prompt wording for every stage** — extracted verbatim to `prompts/*.tmpl` in M0; consumed
  as data by both engines; prompt changes are their own reviewed PRs (ADR-008).
- Artifact formats: `manifest.json` schema, `plan.md`/`adr.md`/`acceptance.md`/`review.md`/
  `verdict.md`, FILE-block emit format, `metrics.jsonl` records (v2 adds fields, never
  changes existing ones — schema `build-2.0`).
- The skill's loop shape: plan → per task (implement → test → review → judge).
- Gate semantics: PASS ⇔ tests green AND avg ≥ threshold (ADR-006).

---

## Milestones

### M0 — Repo & process bootstrap (2–3 sessions)
Go module scaffold (layout per [ARCHITECTURE.md](./ARCHITECTURE.md) §8), CI (build,
`go vet`, tests, prompt-diff check), ADRs 001–011 reviewed & statused, prompts extracted
**verbatim** from v1 `orchestrator/fusion/*.py` into `prompts/*.tmpl`. The **Makefile**
(already in repo root, colored self-documenting `make help`) is the single entry point —
CI runs `make check`; every target the plan references (`build`, `test`, `soak`, `replay`,
`prompts-check`, `docs-check`, `docker-build`, `docker-run`) lives there, and new tooling
gets a target, not a README paragraph.
**Exit gate:** CI green (`make check`) on the skeleton; prompt files byte-diffed against a
v1 extraction script output (`make prompts-check`); ADR statuses set.

**M0 status (2026-07-10):**
- [x] Prompts extracted verbatim (21 blocks, 5 stages; commit `4308387`) — deterministic
  (double-run diff), negative-tested (hand-edit caught), two-layer freeze check
  (`make prompts-check`: checksums always, byte-diff vs v1 when `V1_DIR` present)
- [x] Go scaffold written: `go.mod`, `cmd/local-fusion` (version/serve stub),
  `internal/{version,mcp,jobs,store,engine,engine/providers,sched}` with ADR-annotated
  package docs; `.gitignore`; CI workflow (make-driven + mixed prompt/engine PR guard)
- [x] Makefile toolchain fully containerized (`RUN_GO` → `golang:1.23`) — no host Go
- [x] ADR statuses set (adr/README, 11 accepted)
- [x] **`make check` green** (2026-07-11, host Docker 29.5.2, first run after image pull —
  zero fixes needed): lint clean (`go vet` + `gofmt`), tests pass under `-race`, prompt
  freeze OK on both layers (checksums + byte-identical fresh v1 re-extraction with
  `V1_DIR` mounted), all doc links resolve. **M0 closed.**

### M1 — De-risking spikes (2–4 sessions) — *decides, not builds*
- **S1 (Q1):** Go `net/http` chat completion against Featherless + Ollama Cloud.
- **S2 (Q3):** official Go MCP SDK, Streamable HTTP, `lf_echo`; connect Claude Code, Cline,
  Cursor by URL. Cline is the acceptance bar.
- **S3:** budget kill-switch prototype (context cancellation through a fake 10-min stage).
**Exit gate:** written PASS/FAIL per spike in `adr/` amendments; on FAIL, fallback selected
(curl shim / alt SDK / stdio-first) **before** M2 code.

**M1 status (2026-07-11): GATE CLOSED — all three spikes PASS, no fallbacks invoked.**
Spike code in `spikes/` (own Go module; finding: MCP SDK v1.6.1 needs **Go ≥ 1.25** —
bump `GO_IMAGE` when adopting it at M2 start).
- [x] **S3 kill-switch: PASS** — ADR-007 amendment; `spikes/s3-killswitch` green under
  `-race` (150ms budget kills a wedged 3-stage panel in 0.15s; 20-job cancellation storm,
  zero leaks).
- [x] **S1: FULL PASS** — ADR-001 amendment; plain `net/http` completed authenticated
  chat completions on both providers, Featherless HTTP 200 *through* Cloudflare
  (`cf-ray` present). Q1 answered: no utls/curl-shim fallback needed.
- [x] **S2: PASS, client matrix 3/3** — ADR-002 amendment; `lf_echo` over Streamable
  HTTP from a container, called by URL from Claude Code (scripted), Cline (the bar,
  owner-verified), and Cursor (owner-verified). Streamable HTTP confirmed primary.

### M2 — Pure-Go shell + the quality gate (6–10 sessions) → **pilot starts here**
*(Amended 2026-07-10, owner decision: the "Go shell, Python brain" proxy stage is dropped —
see ADR-001 amendment. Python never enters the v2 image; v1 keeps running host-side.)*
`internal/mcp` (HTTP+stdio), `internal/jobs` (queue, budgets, retry ledger, persistence),
`internal/store` (artifact volume), `lf_job`/`lf_cancel` — plus the two smallest stages
**ported to Go now**: `judge` (gate semantics per ADR-006, dual judge + test gate) and
`review`, both parity-verified via record/replay. Until `plan` ports (M3), briefs enter as
**data from the agent** (consistent with ADR-004's everything-as-data): authored via v1
planning run host-side, or directly from the intent document. Skill updated to submit/poll.
Also here: the **record/replay harness** (v1 `LF_RECORD` mode runs host-side in the v1
checkout; Go replay lives in this repo) and **baseline service observability** (structured
logs with job/stage/provider fields; per-provider error/latency counters in `lf_status`).
**Exit gate:** full gated run (agent implements → tests → `lf_review` → `lf_judge` with test
report) on default agent timeouts; kill-switch test; red-test PASS impossible; judge+review
**record/replay parity green** (ADR-010); **soak test** (20 concurrent fake-provider jobs
under `-race`, 0 leaks/races); **user docs v1** (quickstart, MCP setup per agent, config
reference for everything shipped so far — R15) proven by **pilot engineer #1 onboarding
≤15 min using only `docs/`**, no author help.

### M3 — Port the planning brain, parity-gated (8–14 sessions)
*(judge + review moved into M2 by the 2026-07-10 amendment.)* Order: **plan-solo**
(decompose + haft — unblocks async planning, the original #1 pain) → **plan-full** (TL
panel + synthesizer) → **coder-solo** → **coder-fusion path last and only port, never
improve** (ADR-009). No engine switch needed — there is no Python in v2 to switch from;
v1 host-side remains the fallback until each stage's parity is green.
**Exit gate per stage (deterministic — ADR-010):** record/replay request parity vs v1 on
the T25 reference + synthetic cases; byte-comparable artifacts on canned responses, including
injected-failure degradation paths; metrics records equivalent. One live run per stage as
**advisory smoke** (judge scores recorded, flagged if outside historical noise — never the
gate; the judge bench is a documented SPOF and stays off the critical path).

### M4 — v2-only features (4–6 sessions)
Hot reload (R8), stage-granular progress (R10), rubric config (R9 — only with pilot
feedback in hand), provider `anthropic` client if not already exercised.
**Exit gate:** PRD success-criteria checklist §6 leading indicators measurable.

### M5 — Decommission & handoff (2–3 sessions)
v1 marked legacy-for-experiments (its host-side planning role ends when M3's plan parity is
green), docs updated, team default decision.
**Exit gate:** one month of pilot metrics reviewed; go/no-go on team-wide default written up.

**Critical path:** M1 → M2. M3 can interleave with pilot feedback. If time collapses:
ship M2 and stop — the pure-Go quality gate alone is a complete, useful product; only
async *planning* waits on M3.

## Engineering process

**Source & branching.** Trunk-based: short-lived branches → PR → squash-merge to `main`.
No direct pushes to `main` once M2 lands.

**ALL commands and tools run in containers, always (owner mandate, 2026-07-10).** Go runs
in `RUN_GO` (`golang:1.23`), scripts in `RUN_PY` (`python:3.12-slim`), the product ships as
a Docker image; contributor host requirements are exactly **docker + make**. CI runs the
same make targets — "works locally, fails in CI" is structurally impossible. Nobody installs
Go, Python, or any other toolchain on a host to work on local-fusion.

**Definition of Done (every PR):** code + tests (unit for pure logic, integration for
tool surface); CI green; no prompt-file changes in the same PR as engine changes (prompt
changes are their own PR with rationale — they're the product); docs updated if behavior
changed — for user-visible changes that means `docs/` (R15), not just product-docs;
self-review with the v1 `code-review` discipline.

**User documentation (R15).** User docs live in `docs/` (docs-as-code, same repo, versioned
with the binary) — distinct from `product-docs/`, which is the build plan for implementers.
Docs are written **with** the feature, never scheduled "after"; a user-visible feature
without its docs does not pass DoD. Structure per R15: quickstart, how-it-works,
benefits/evidence, configuration reference with annotated examples, MCP setup per agent,
tool reference, troubleshooting/FAQ. The standing quality test: someone who didn't build it
reaches a first gated run using only `docs/` — first enforced at the M2 exit gate,
re-verified at M5.

**Dogfooding rule.** From M2 exit onward, every local-fusion feature ≥1 session of effort is
built *through* the gate itself (plan → implement → test report → judge). The tool is its own
first pilot; gate failures on our own PRs are logged as product feedback.

**ADR discipline.** Any decision that (a) reverses an existing ADR, (b) adds a dependency, or
(c) changes a tool contract requires a new/amended ADR before code. Statuses: Proposed →
Accepted → Superseded. Spike results amend ADRs 001/002 with evidence.

**Testing strategy (summary).** Unit: pure engine logic (gate, budgets, parsers, resolvers) —
no network. Contract: MCP tool schemas snapshot-tested (a signature change is a reviewable
diff). Integration: fake-provider harness (canned model responses) for full-stage runs in CI.
Parity: **deterministic record/replay per ADR-010** (request parity + canned-response artifact
parity), in CI. **Concurrency correctness:** all tests run under `go test -race`; the job
runner additionally gets a soak test (sustained concurrent jobs, cancellation storms, budget
expiries) as an M2 exit gate — it is the riskiest net-new code and gets proportional testing.
Live smoke: one real gated run before closing any milestone (advisory judge scores recorded).

**Cadence & tracking.** GitHub issues per milestone with the exit-gate checklist as the
issue body; weekly 15-min self-review (what closed, what's blocked, does the plan need
updating — the plan is a living doc); pilot feedback captured as issues labeled `pilot`,
triaged before new feature work.

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Cloudflare blocks Go's HTTP client on Featherless | Low–medium | M1 S1 spike day 1; fallback: utls or curl-exec shim behind the provider interface |
| Go MCP SDK gaps (Streamable HTTP vs Cline) | Medium | M1 S2 spike across all 3 agents; fallback stdio-first is a **named partial retreat** (ADR-002 amendment), not an equivalent |
| Prompt drift during port | Medium | M0 verbatim extraction + CI byte-diff; prompts-as-data (ADR-008) |
| Parity judged by noisy LLM scores | Eliminated | Deterministic record/replay parity (ADR-010) |
| Job-runner concurrency bugs | Medium | `-race` everywhere + M2 soak-test exit gate |
| Account-level provider contention (multi-client) | Low (pilot) | Capacity policy in ADR-002 amendment: shared key ⇒ shared server; per-dev servers ⇒ per-dev keys |
| Container egress / data governance blocks sign-off | Medium | [DATA-GOVERNANCE.md](./DATA-GOVERNANCE.md) checklist is a pilot-widening precondition |
