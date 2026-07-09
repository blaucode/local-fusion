# local-fusion v2.0 — Plan

> v1.0 proved the thesis: multi-model deliberation beats a solo model on hard, underspecified
> tasks, at flat-rate cost. v2.0 turns the working prototype into a proper **engineered loop**:
> a Go engine, a containerized async MCP server, first-class termination/budget guards, and a
> memory feedback loop — while deliberately NOT investing in the parts v1 proved don't pay.
> Created: 2026-07-07

---

## What v2.0 is

Same product idea as v1 — a multi-model "thinking engine" (plan deliberation → coder →
review → dual-judge gate) driven by whatever coding agent you already use — rebuilt on
infrastructure that matches how the loop actually runs:

- **Go engine, single static binary.** Kills the python3.14/pip/interpreter-mismatch setup
  pain and makes parallel panel calls natural (goroutines, not ThreadPoolExecutor + curl).
- **Containerized MCP server, Streamable HTTP transport.** One long-lived server, any number
  of agents/projects connecting over HTTP. No more per-agent stdio spawn.
- **Async job model.** `lf_plan` and `lf_coder_fusion` take minutes; v1's `timeout: 3600`
  stopgap becomes submit → poll. Long stages stop fighting MCP client timeouts.
- **Stricter isolation.** v2 tightens v1's best invariant ("never write the source tree") to
  "never touch the host at all": the engine owns its artifact store, returns everything as
  data, and git operations move to the agent/skill side.

## Why (the one-paragraph justification)

The industry named what this project already is: **loop engineering** — designing the system
that prompts, checks, remembers, and re-runs an agent instead of prompting by hand. v1 scores
well on the hard parts (verification gate, external state, honest measurement) and poorly on
the operational parts (sync transport, no budgets, memory that's written but never read back).
v2.0 closes exactly those gaps. See [01-loop-engineering-assessment.md](./01-loop-engineering-assessment.md).

## Document map

| Doc | Contents |
|---|---|
| [01-loop-engineering-assessment.md](./01-loop-engineering-assessment.md) | Is v1 "loop engineering"? Gap analysis. What v1 does right / focus / distractions. |
| [02-product-vision.md](./02-product-vision.md) | v2.0 product idea, target user, value proposition, scope. |
| [03-architecture.md](./03-architecture.md) | Go engine, containerized MCP server, async jobs, artifact store, new tool surface. |
| [04-migration-plan.md](./04-migration-plan.md) | Phased Python → Go migration, what ports first, parity gates. |
| [05-roadmap.md](./05-roadmap.md) | Prioritized roadmap: build / keep-cheap / drop. |
| [06-open-questions.md](./06-open-questions.md) | Unresolved design decisions to settle before coding. |
| [07-team-adoption.md](./07-team-adoption.md) | **Team edition**: model-agnostic, quality-gate-first strategy for handing the tool to engineering + architecture. |

## v1 → v2 at a glance

| Dimension | v1.0 (working today) | v2.0 (planned) |
|---|---|---|
| Language | Python 3.14 + curl subprocess | Go, single binary |
| Transport | stdio MCP, sync, per-agent process | Streamable HTTP MCP, containerized, shared |
| Long stages | Client timeout raised to 3600s | Async jobs: submit → poll → result |
| Git ops | Engine creates `feature/<slug>` branch | Moved to agent/skill — engine never touches host |
| Artifacts | Written into `<project>/local-fusion/<slug>/` | Engine-owned store; returned as data; agent materializes |
| Termination | PASS ≥ 8.0, one judge retry (skill convention) | Enforced budgets: step caps, wall-clock, token, no-progress |
| Verification | Agent runs tests; judge doesn't see results | Test report is a required judge input; tests-green gates PASS |
| Memory | metrics.jsonl written, never read back | Distilled lessons injected into planning prompts (Reflexion) |
| Model evals | Manual `discover` / `eval` runs | Scheduled automation + registry auto-update proposal |
| Config | Read once at startup; restart to change | Hot-reload / `lf_reload` |

## Non-goals for v2.0

- Building coder-fusion *variants* before validating the baseline. Its value was never
  isolated from planning in v1 — a cheap pre-registered ablation (roadmap #8) decides its
  fate; no optimization until it reports.
- Growing the reviewer panel (v1 proved it can't catch design gaps; it stays a cheap
  conformance check).
- A hosted/multi-tenant product. v2.0 is local-first; "team" means shared config and a
  container a colleague can run, not a SaaS.
- New pipeline stages for the team before demand exists — team v1 is the **quality gate
  only**; everything else graduates on request (see 07-team-adoption.md §5).
