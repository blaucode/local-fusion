# local-fusion v2

A multi-model quality gate for coding agents: independent plan deliberation, gated
implementation (deterministic test gate + dual LLM judges), and a full artifact paper trail —
served as a containerized async MCP server written in Go.

**Status:** planning complete, implementation not started. First milestone: **M0**.
**v1 (working reference implementation, Python):** `vendo/local-fusion` — stays operational
throughout the migration.

## Start here

| If you are… | Read |
|---|---|
| **Implementing** (human or agent) | [AGENTS.md](./AGENTS.md) — read order, rules, tools |
| Reviewing the product | [product-docs/PRD.md](./product-docs/PRD.md) |
| Reviewing the plan | [product-docs/PROJECT-PLAN.md](./product-docs/PROJECT-PLAN.md) |
| Reviewing decisions | [product-docs/adr/](./product-docs/adr/README.md) |

## product-docs/

| Doc | One line |
|---|---|
| [PHILOSOPHY.md](./product-docs/PHILOSOPHY.md) | The doctrine: the spec-anchored loop, our place in the loop-engineering stack, proportional intent, the three debts |
| [PRD.md](./product-docs/PRD.md) | Problem, goals, requirements (P0/P1/P2), success metrics, scope decisions |
| [PROJECT-PLAN.md](./product-docs/PROJECT-PLAN.md) | Milestones M0–M5 with exit gates, engineering process, port contract, risk register |
| [ARCHITECTURE.md](./product-docs/ARCHITECTURE.md) | Topology, tool surface, job model, store, Go layout, container |
| [adr/](./product-docs/adr/README.md) | 10 architecture decision records (the *why* behind every load-bearing choice) |
| [DATA-GOVERNANCE.md](./product-docs/DATA-GOVERNANCE.md) | Code egress per cost profile, threat model, sign-off checklist |
| [OPEN-QUESTIONS.md](./product-docs/OPEN-QUESTIONS.md) | Decision log; Q1/Q3 are open **on purpose** — they're M1 spikes |
| [TEAM-ADOPTION.md](./product-docs/TEAM-ADOPTION.md) | Pilot strategy, graduation ladder, anti-over-engineering guardrails |
| [BACKGROUND.md](./product-docs/BACKGROUND.md) | v1 assessment against loop-engineering principles; what carries over and why |
