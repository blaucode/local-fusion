# Is local-fusion "loop engineering"? — Assessment of v1.0

> Verdict: **yes, substantially — and earlier than the trend.** local-fusion is an engineered
> agent loop with a verification gate, external state, and honest measurement. What it's
> missing is the *operational* half of the discipline: async execution, enforced termination
> budgets, memory that feeds back, and automation. Those are v2.0.
> Created: 2026-07-07
>
> **Historical snapshot -- read for rationale, not for tasks.** This assessment predates
> the PRD/plan/ADRs, which supersede it wherever they differ (the doctrine now lives in
> PHILOSOPHY.md, and section 6's hygiene items were completed 2026-07-08/09: the v1 doc-12
> file exists, CLAUDE.md was addressed, the pilot kit moved). Nothing in here is a to-do.

---

## 1. What "loop engineering" means

The term crystallized in June 2026 (Peter Steinberger's viral post, then Addy Osmani's essay
naming it): the skill is no longer prompting an agent but **designing the system that prompts,
checks, remembers, and re-runs it**. A well-engineered loop has:

1. A clear goal with a **testable termination condition**.
2. Tools that touch the **real environment** (files, tests, git) so feedback is honest.
3. **Context management** — compaction, pruning, externalized state.
4. **Termination & escalation logic** — success/failure exits, budgets, no-progress detection,
   escalate to a human instead of burning tokens.
5. **Verification as the reward signal** — deterministic checks (tests, linters) first;
   LLM-as-judge only for what can't be mechanically checked.

Osmani's structural anatomy adds: automations, worktrees, skills, connectors, sub-agents,
external state. Relevant research patterns: ReAct (act ↔ observe), Reflexion (write lessons
from failures into memory that future runs read), Evaluator-Optimizer (generator + evaluator
cycling — Anthropic's *Building Effective Agents*), Orchestrator-Workers.

## 2. Mapping v1.0 onto the anatomy

| Loop-engineering element | v1.0 status | Evidence |
|---|---|---|
| Testable termination condition | ✅ Strong | Dual-judge gate, PASS ≥ 8.0, calibrated; full test suite as objective signal |
| Real-environment tools | ✅ Strong | Agent applies files, runs tests; engine proposes only (safe by design) |
| External state | ✅ Strong | Artifact folder (`manifest.json`, plan/review/verdict), `metrics.jsonl` |
| Sub-agents / maker-vs-checker split | ✅ Strong | TL panel, reviewer panel, independent dual judges — evaluator-optimizer done right |
| Skills | ✅ | One SKILL.md deployed identically to Claude/Cline/Cursor |
| Connectors (MCP) | ✅ | 5-tool MCP server as the primary surface |
| Honest measurement | ✅ Exceptional | Pre-registered v2.0 experiment, null results kept, judge-artifact traps documented |
| Context management | ⚠️ Partial | Decision-table synthesis input fixed the 32K overflow, but 32K remains the hard ceiling; no general compaction strategy |
| Termination/budget **enforcement** | ⚠️ Weak | "One judge retry then stop" lives in the skill prompt — a convention, not enforced. No step caps, wall-clock, token budgets, or no-progress detection in the engine |
| Deterministic verification **in the loop** | ⚠️ Partial | The agent runs tests, but the judge never sees the results — an LLM gate can PASS code whose tests fail |
| Memory that feeds back (Reflexion) | ❌ Missing | metrics.jsonl and agentmemory are written religiously but nothing reads them back into planning prompts |
| Automations | ❌ Missing | Every run is human-initiated; `discover`/`eval` are manual |
| Worktrees / parallel isolation | ❌ Missing | One feature branch, agent works in place; no parallel task execution |
| Async execution | ❌ Missing | Sync stdio tools taking minutes; `timeout: 3600` stopgap |

**Score: v1.0 nails the epistemics of loop engineering (verify, measure, don't trust the
model's self-report) and lags on the operations (run unattended, in parallel, within budgets,
learning from its own history).** That's the right order to have gotten them in — the
epistemics are the hard part to retrofit.

## 3. What v1.0 is doing right (keep these — they're the moat)

1. **The division of labor.** "local-fusion proposes, the agent applies; the engine never
   writes the source tree" is exactly the maker/checker separation and human-in-control gate
   the literature recommends. It also makes containerization (v2) nearly free.
2. **Verification-first culture.** Dual judges because single judges are noisy; structured
   JSON verdicts; cross-validation that exposed judge artifacts (T17); a quality gate the
   agent can't argue with. Most "agent pipeline" projects have none of this.
3. **Measure everything, including nulls.** Pre-registration, frozen stopping criteria,
   metrics.jsonl, keeping the v2/v3 null results. This is what makes the project's claims
   trustworthy — and it's rare.
4. **Effort scaled to difficulty.** The `solo`/`no_fusion` flags and the honest finding that
   fusion only pays on ≥5-ambiguity tasks. Many loop designs burn full cost on every task.
5. **The model registry + eval harness.** Role-scored fitness, `discover`/`eval`, promotion
   criteria (≥8.5 to judge). This is the "explore open-source models" half of the project,
   and it's already systematized — it just isn't scheduled yet.
6. **Economic clarity.** Flat-rate providers → "model diversity is free, so use it." The
   whole design follows from one true premise.

## 4. What to focus on (v2.0 priorities, in order)

1. **Async job model + Streamable HTTP + container** — the #1 operational bottleneck today
   (minutes-long sync calls vs 60s client timeouts). This is the Go rewrite's real payload.
2. **Enforced termination & budgets** — move "retry once, then stop" from prompt convention
   into engine-enforced state: step caps, wall-clock/token budgets per job, no-progress
   detection, structured recoverable-vs-fatal errors.
3. **Deterministic verification wired into the gate** — `lf_judge` requires a test report;
   PASS = tests green AND score ≥ 8.0. Never let the LLM gate override the compiler/tests.
4. **Close the memory loop (Reflexion)** — distill metrics.jsonl + verdicts into a small
   `lessons.md` injected into planning prompts. v1 found a *systematic* blind spot (untested
   non-integer JSON input flagged by judges twice) that a lessons file would have fixed once.
5. **Automate discover/eval** — a scheduled job that finds new provider models, benchmarks
   them, and proposes registry updates. Turns "explore open-source models" from a hobby-time
   activity into part of the loop.

## 5. What is a distraction (spend nothing / keep cheap)

1. **Optimizing coder-fusion before validating it.** Epistemic status: **unvalidated, not
   disproven.** The v1 experiments never isolated the coder stage — every win compared solo
   vs *full* fusion, so attributing the gains to planning is itself an inference from
   small-n, noisy-judge data. The distraction is investing in coder-fusion variants (new
   merge strategies, new coder pairs) *before* running the missing ablation: same
   synthesized brief, coder-fusion arm vs single-coder arm, 2–3 pre-registered task pairs.
   Cheap (planning is shared), settles it either way. Until then: behind a flag, keep
   logging `cf-1.0` metrics, don't port its complexity to Go first.
2. **Growing the reviewer panel.** Proven negative result: reviewers can't catch design gaps
   (3/3 passed code with a known coverage hole) and produced the false-positive classes v1
   spent effort suppressing. It stays a cheap conformance check — nothing more.
3. **Big-bang Go rewrite.** The risk isn't Go, it's rewriting *everything at once*. The
   pipeline's value is in prompts, config, and hard-won conventions — keep those as data
   files, port the engine incrementally with parity gates (see PROJECT-PLAN.md).
4. **Manually chasing every new model.** The eval harness makes each benchmark cheap, but
   human-initiated model-hopping is time that doesn't improve the loop. Automate it (focus
   #5) and let the registry decide.
5. **Surface proliferation.** Three agent skill deployments are fine because it's one file.
   Resist per-agent divergence, more CLI subcommands, or a web UI before the core is async.

## 6. Documentation hygiene (small, do during v2)

- `documentation/12-roadmap-and-limitations.md` is referenced 4× but **does not exist** —
  this v2.0 folder should replace it (or add a stub pointing here).
- `CLAUDE.md` is stale by its own admission (model lineup, status checkboxes from June).
  AGENTS.md is canonical; consider trimming CLAUDE.md to memory rules + a pointer.
- The older standalone CLI scripts (`orchestrator/plan.py`/`review.py`/`judge.py` pre-engine
  paths) are legacy; mark them deprecated so the Go port doesn't reproduce them.
