# local-fusion v2.0 — Product Vision

> One sentence: **a self-operating quality loop for coding agents** — plan deliberation,
> gated implementation, and continuous model discovery, running as a service you connect to
> rather than a script you babysit.
> Created: 2026-07-07

---

## 1. The product idea, restated

v1 answered "*does* multi-model fusion of cheap open-weight models beat a solo model?"
(Yes — on hard, underspecified tasks; no — on simple ones.) v2 answers the product question:
"*how does that capability run day-to-day without a human operating it?*"

v2.0 is a **long-lived, containerized service** that any coding agent (Claude Code, Cline,
Cursor) connects to over Streamable HTTP MCP. The agent stays the hands (context, files,
tests, git); local-fusion stays the brain (deliberation, panels, judges) — but now the brain:

- accepts **jobs** instead of blocking calls, so hour-long deliberations are normal, not a
  timeout hack;
- enforces its own **budgets and stopping rules**, so a runaway loop is impossible by
  construction;
- **remembers** — distilled lessons from past verdicts shape the next plan;
- **maintains its own model bench** — scheduled discovery and evals keep the registry current
  without anyone remembering to run them.

## 2. Who it's for (unchanged, sharpened)

- **The team** (software engineering + architecture) — the primary v2 target. Model-agnostic:
  engineers keep their existing agents and API keys; the tool adds the gate and the paper
  trail. Entry point is the quality gate alone (see [07-team-adoption.md](./07-team-adoption.md)).
- **The individual engineer on flat-rate subscriptions** (Featherless $25/mo + Ollama Cloud
  pro) — the original persona, now served by a `flat-rate` cost profile where model
  diversity is free.
- **Architects wanting evidence**: every agent-built feature ships with a brief, review,
  verdict, and metrics — reviewable in the PR instead of reverse-engineered from the diff.
- Reference target remains **Symfony/PHP backend work** (the T-series task bank), but nothing
  in the engine is PHP-specific; the Go core must stay language-agnostic.

## 3. Value proposition delta vs v1

| | v1.0 | v2.0 |
|---|---|---|
| Quality | Fusion gate on hard tasks | Same, plus lessons feedback → fewer repeat blind spots |
| Cost | Flat-rate inference | Same, plus enforced token/time budgets per job |
| Operation | Babysit minutes-long sync calls; restart server on config change | Fire-and-forget jobs; hot-reload; runs in Docker on a NAS/server if you want |
| Safety | Never writes source tree | Never touches the host at all (container boundary + no git ops) |
| Model exploration | Manual discover/eval sessions | Scheduled; registry proposals arrive as artifacts |

## 4. Core scenarios

**S1 — Gated feature build (the main loop, unchanged in shape).** Agent gathers context →
`lf_plan` (job) → per task: `lf_coder_fusion` (job) → agent applies + tests → `lf_review` →
`lf_judge` with the test report attached → PASS or one engine-tracked retry → next task.

**S2 — Overnight planning.** Agent submits three feature requests as plan jobs before you
log off; you review three deliberated briefs (ADR + plan + acceptance) in the morning.
Planning is the proven-value stage — this is the "runs while you sleep" scenario that
actually pays, without letting an agent write code unattended.

**S3 — Continuous model bench.** A scheduled job runs `discover` weekly, `eval`s new
candidates against the reference implementation, and writes a proposal artifact ("qwen4-72b
scored 8.9 as judge — promote?"). You approve; registry updates; server hot-reloads.

**S4 — Cross-project memory.** Judge findings across projects distill into per-stack lessons
("Symfony: always test non-integer JSON input") that the planner injects automatically.

## 5. Scope boundaries

**In scope for v2.0:** Go engine at v1 parity (plan/coder-fusion/review/judge/status), async
job model, Streamable HTTP transport, container image, enforced budgets, test-report-gated
judging, lessons feedback, scheduled discover/eval, hot config reload.

**Out of scope:** multi-tenant auth, web dashboard, new providers, autonomous application of
code (the agent always applies), coder-fusion optimization, IDE plugins.

## 6. Success criteria

1. A full S1 run on a real task with zero timeout configuration in any agent.
2. Kill-switch test: a job with a 5-minute budget provably stops at 5 minutes with a clean
   partial artifact and a `budget_exhausted` status.
3. A judge PASS is impossible when the attached test report has failures.
4. One documented case where an injected lesson prevented a previously-logged blind spot.
5. `docker run` + one MCP config block = working setup on a machine with no Python.
