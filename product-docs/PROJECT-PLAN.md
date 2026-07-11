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
`go vet`, tests, prompt-diff check), ADRs 001–010 reviewed & statused, prompts extracted
**verbatim** from v1 `orchestrator/fusion/*.py` into `prompts/*.tmpl`.
**Exit gate:** CI green on empty skeleton; prompt files byte-diffed against a v1 extraction
script output; ADR statuses set.

### M1 — De-risking spikes (2–4 sessions) — *decides, not builds*
- **S1 (Q1):** Go `net/http` chat completion against Featherless + Ollama Cloud.
- **S2 (Q3):** official Go MCP SDK, Streamable HTTP, `lf_echo`; connect Claude Code, Cline,
  Cursor by URL. Cline is the acceptance bar.
- **S3:** budget kill-switch prototype (context cancellation through a fake 10-min stage).
**Exit gate:** written PASS/FAIL per spike in `adr/` amendments; on FAIL, fallback selected
(curl shim / alt SDK / stdio-first) **before** M2 code.

### M2 — Go shell, Python brain (6–10 sessions) → **pilot starts here**
`internal/mcp` (HTTP+stdio), `internal/jobs` (queue, budgets, retry ledger, persistence),
`internal/store` (artifact volume), `lf_job`/`lf_cancel`; stages proxied to the v1 Python CLI
(pinned checkout/submodule). Skill updated to submit/poll. Plus (external-review additions):
the **record/replay harness** (Python `LF_RECORD` mode + Go replay — needed by M3 anyway,
built here) and **baseline service observability** (structured logs with job/stage/provider
fields; per-provider error/latency counters exposed in `lf_status` — enough to answer "why is
this job slow" on a box others depend on). Ugly on purpose — ships the async fix and R1–R5
without touching validated pipeline logic.
**Exit gate:** full gated run (S1 scenario) on default agent timeouts; kill-switch test;
red-test PASS impossible; **soak test** (20 concurrent fake-provider jobs under `-race`,
0 leaks/races); **pilot engineer #1 onboarded ≤15 min.**

### M3 — Port the brain, parity-gated (8–14 sessions)
Order: judge → review → coder-solo → plan (risk-ascending; coder-fusion path last and only
port, never improve — ADR-009). Each stage behind `engine: go|python`.
**Exit gate per stage (deterministic — ADR-010):** record/replay request parity vs Python on
the T25 reference + synthetic cases; byte-comparable artifacts on canned responses, including
injected-failure degradation paths; metrics records equivalent. One live run per stage as
**advisory smoke** (judge scores recorded, flagged if outside historical noise — never the
gate; the judge bench is a documented SPOF and stays off the critical path).

### M4 — v2-only features (4–6 sessions)
Hot reload (R8), stage-granular progress (R10), rubric config (R9 — only with pilot
feedback in hand), provider `anthropic` client if not already exercised.
**Exit gate:** PRD success-criteria checklist §6 leading indicators measurable.

### M5 — Decommission & handoff (2–3 sessions)
Python proxy off, v1 marked legacy-for-experiments, docs updated, team default decision.
**Exit gate:** one month of pilot metrics reviewed; go/no-go on team-wide default written up.

**Critical path:** M1 → M2. M3 can interleave with pilot feedback. If time collapses:
ship M2 and stop — it alone retires v1's worst limitation.

## Engineering process

**Source & branching.** Trunk-based: short-lived branches → PR → squash-merge to `main`.
No direct pushes to `main` once M2 lands.

**Definition of Done (every PR):** code + tests (unit for pure logic, integration for
tool surface); CI green; no prompt-file changes in the same PR as engine changes (prompt
changes are their own PR with rationale — they're the product); docs updated if behavior
changed; self-review with the v1 `code-review` discipline.

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
