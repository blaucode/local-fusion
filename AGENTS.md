# AGENTS.md — instructions for coding sessions in this repo

This is the **canonical context document** for any agent (or human) implementing
local-fusion v2. CLAUDE.md defers to this file. Read it fully before writing code.

## What you are building

A Go, containerized, async MCP server exposing a multi-model quality gate for coding agents.
Planning is **complete and reviewed**; your job is execution against
[product-docs/PROJECT-PLAN.md](./product-docs/PROJECT-PLAN.md). Do not re-plan.

## Read order (do this at session start)

1. [product-docs/PHILOSOPHY.md](./product-docs/PHILOSOPHY.md) — the doctrine (short). The
   one-sentence version: this is a **spec-anchored** verification loop — it must never run
   without human-owned intent, deterministic checks always outrank models, and machines may
   draft intent but never approve it. When a design choice feels ambiguous, this doc breaks
   the tie.
2. [product-docs/PROJECT-PLAN.md](./product-docs/PROJECT-PLAN.md) — find the **current
   milestone** (lowest M whose exit gate isn't checked off) and its exit-gate checklist.
   That checklist is your task list. Also read: **Port contract** (what must not change),
   **Engineering process**, **Risk register**.
3. [product-docs/PRD.md](./product-docs/PRD.md) — requirements R1–R15 (R14 retired) with
   acceptance criteria; the Appendix lists what is deliberately out of scope. Don't build
   P2 items.
4. [product-docs/ARCHITECTURE.md](./product-docs/ARCHITECTURE.md) — component layout, tool
   surface, job model, store schema, Go package layout (§8).
5. [product-docs/adr/README.md](./product-docs/adr/README.md) — skim the index; **read the
   full ADR before touching anything it governs** (e.g. touching the job runner → ADR-003 +
   007; provider client → ADR-008; parity harness → ADR-010; `lf_plan` contract → ADR-011).
6. Only when relevant: DATA-GOVERNANCE (security-adjacent work), ADOPTION (rollout/pilot
   work), OPEN-QUESTIONS (before "resolving" anything — Q1/Q3 are answered by **spikes**,
   never by assumption), BACKGROUND (history/rationale).

## The reference implementation

v1 lives at `../../vendo/local-fusion` (sibling checkout). `orchestrator/fusion/*.py` is the
**source of truth** for prompts, provider semantics, artifact formats, and degradation
behavior. When v2 behavior is ambiguous, v1's behavior wins (that's what parity means).
Never port from memory — read the Python.

## Non-negotiable rules

1. **Milestone order.** M-gates are sequential. Do not start M2 work before M1 spikes have
   written results into ADR-001/002. Do not close a milestone without its exit gate passing.
2. **Prompts are frozen data.** All stage prompts live in `prompts/*.tmpl`, extracted
   verbatim from v1. Never paraphrase, "improve", or inline them in Go. Prompt changes are
   their own PR, never mixed with engine changes (CI enforces).
3. **Parity is deterministic** (ADR-010): record/replay request parity + canned-response
   artifact parity. Judge scores are advisory smoke only — never a gate you tune to.
4. **Gate semantics are sacred** (ADR-006): PASS ⇔ test exit_code 0 AND avg ≥ threshold.
   No code path may let a model verdict override the test runner.
5. **The server never touches host filesystem or git** (ADR-004). If you find yourself
   adding a repo path to the server, stop — that belongs in the skill/agent.
6. **ADR-before-code** for anything that reverses an ADR, adds a dependency, or changes a
   tool contract. Amend, don't silently diverge.
7. **Concurrency code runs under `-race`**, and the job runner requires the soak test
   (M2 exit gate) before it's considered done.
8. **User docs ship with the feature** (PRD R15). Anything user-visible you build or change
   gets its `docs/` update in the same PR — quickstart, config reference, MCP setup, tool
   reference live there. `product-docs/` is for implementers; `docs/` is for users; never
   confuse the audiences. A feature without docs does not pass DoD.
9. **Definition of Done** is in PROJECT-PLAN — tests, CI green, docs updated, no mixed
   prompt/engine PRs.
10. **ALL commands and tools run in containers, always** (owner mandate, 2026-07-10). Go
   runs in `RUN_GO` (`golang:1.23`); scripts run in `RUN_PY` (`python:3.12-slim`); the
   product ships as a Docker image. Host requirements are exactly: **docker + make**. If a
   task seems to need `apt install` / `brew install` / any language runtime on the host,
   stop and route it through a container via a make target instead. Never invoke `go`,
   `python3`, or other toolchain binaries directly on the host.

## Tools to use in your loop

- **codegraph** (if available): index this repo and the v1 reference repo at session start;
  use it to trace call paths before editing (especially `orchestrator/fusion/*.py` when
  porting — port from the graph and the source, not from doc summaries).
- **agentmemory** (if available): `memory_save` immediately after every meaningful result —
  spike outcomes, exit-gate evidence, parity results, gotchas (e.g. provider quirks, SDK
  bugs), and any decision made mid-session. Do not batch or defer saves; a finding lost at
  session end is unrecoverable. Search memory at session start for prior findings on your
  current milestone.
- **haft** (if available): use for structured deliberation (frame → explore → compare)
  before resolving any OPEN-QUESTIONS item or drafting an ADR amendment. If unavailable,
  follow the same three-step structure manually in the ADR's Options/Trade-offs sections.

## Progress tracking

- Check off exit-gate items in PROJECT-PLAN.md as evidence lands (link the evidence: test
  run, CI job, spike result).
- Spike results (M1) are written into ADR-001/ADR-002 as amendments with dates.
- Keep `metrics.jsonl` discipline from day one: every live run logged.

## What NOT to do

Don't add features not traceable to a PRD requirement. Don't resolve Q1/Q3 from training
data — run the spike. Don't "clean up" v1 while porting. Don't touch `vendo/local-fusion`
except to read it or run its recorded-mode harness. Don't commit secrets — provider keys
come from env/`providers.env` (gitignored) only.
