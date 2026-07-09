# local-fusion v2.0 — Migration Plan (Python → Go)

> Principle: **port the shell first, the brain last, and never both at once.** The Python
> engine keeps working throughout; every phase has a parity gate measured with the existing
> eval/metrics machinery. No big-bang rewrite.
> Created: 2026-07-07

---

## 0. What must NOT change

- `providers.yaml` schema (registry, pipelines, panels, roles) — the Go loader reads the
  same file.
- Prompt wording for every stage — extracted verbatim from `orchestrator/fusion/*.py` into
  `prompts/*.tmpl` first, then consumed by both implementations during transition.
- Artifact formats: `manifest.json` schema, `plan.md`/`adr.md`/`acceptance.md`/`review.md`/
  `verdict.md`, FILE-block emit format, `metrics.jsonl` records.
- The skill's loop shape (plan → per task: coder-fusion → apply → test → review → judge).

## Phase 1 — De-risk the unknowns (days, not weeks)

1. **Provider connectivity from Go**: hit Featherless + Ollama with `net/http` (TLS, UA,
   streaming). v1 needed curl because Cloudflare blocked urllib; confirm Go passes. If not:
   curl-exec shim behind the provider interface.
2. **Go MCP SDK + Streamable HTTP** smoke test against Claude Code, Cline, Cursor — one
   trivial tool, confirm all three clients connect to a URL-configured server.
3. **Extract prompts to files** (Python side reads them too — a pure refactor that pays
   immediately and freezes the asset).

Gate: all three agents call a Go `lf_echo` over HTTP; one real model call per provider from Go.

## Phase 2 — The shell: server, jobs, store

Build `internal/mcp`, `internal/jobs`, `internal/store`, budgets, `lf_job`/`lf_cancel`/
`lf_reload` — with the engine stages **proxied to the existing Python CLI** (`exec` the v1
modules inside the container or alongside it). Ugly on purpose: it ships the async/timeout
fix (the #1 pain) without touching pipeline logic.

Gate: full S1 run through the Go server (Python brain) with zero agent timeout config;
budget kill-switch test passes.

## Phase 3 — Port the brain, stage by stage

Order by risk (lowest first), each stage behind a `engine: go|python` config switch:

1. `judge` — smallest, structured JSON output already specified, easiest to A/B.
2. `review` — same shape as judge.
3. `coder_fusion` — port the *solo* path first (one coder); fusion path last (it's the
   unproven part anyway — see roadmap).
4. `plan` — biggest and most valuable; port haft/TL-panel/synthesis last, when the provider
   client and concurrency semantics are proven.

Parity gate per stage: run Go and Python on the same inputs (T25 reference + one live task);
judge scores within noise (±0.5), artifacts structurally identical, metrics records
equivalent. Keep both engines runnable until all four pass.

## Phase 4 — v2-only features

Test-report-gated judging, lessons distillation + injection, scheduler (discover/eval cron),
agent-side gitops in the skill, artifact materialization by the agent.

Gate: success criteria 1–5 in [02-product-vision.md](./02-product-vision.md).

## Phase 5 — Decommission

Remove Python proxy path, mark `orchestrator/` legacy (keep for reproducing v1 experiments),
update AGENTS.md + documentation/, add `documentation/12-roadmap-and-limitations.md` stub
pointing at `v2.0/`.

## Anti-goals during migration

- No prompt "improvements" while porting — parity first, iterate after.
- No new providers, no new pipeline stages, no coder-fusion changes.
- No skill rewrite until Phase 2 lands (then it's ~10 lines: submit/poll instead of wait).

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Cloudflare blocks Go HTTP client | Low–medium | Phase 1 test; curl shim fallback |
| Go MCP SDK gaps (Streamable HTTP maturity) | Medium | Phase 1 smoke test across all 3 agents; stdio fallback flag |
| Prompt drift during port | Medium | Prompts as shared template files, diffed in CI |
| Parity judged by noisy LLM scores | Medium | Use dual-judge averages + structural artifact diffs, not single scores |
| Container can't reach Ollama/Featherless from user's network | Low | Plain HTTPS egress only; document proxy env vars |
