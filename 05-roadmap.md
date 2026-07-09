# local-fusion v2.0 — Roadmap & Priorities

> Three buckets: **Build** (v2.0 core), **Keep cheap** (working, don't invest), **Drop/defer**
> (proven distractions). Ordering inside "Build" is by pain × proven value.
> Created: 2026-07-07

---

## Build (in this order)

| # | Item | Why now | Done when |
|---|---|---|---|
| 1 | Async job model + Streamable HTTP server (Go shell, Phases 1–2) | The `timeout: 3600` hack is the biggest daily operational pain; blocks every other improvement | Full run with zero timeout config in any agent |
| 2 | Engine-enforced budgets & termination (wall-clock, step caps, judge-retry ledger, no-progress) | Turns skill-prompt conventions into guarantees; prerequisite for any unattended use | Kill-switch test passes; 3rd judge attempt refused |
| 3 | Test-report-gated judging — **✅ SHIPPED 2026-07-08 in v1 Python** (didn't need to wait for Go): `lf_judge` accepts `test_report`; non-zero exit forces FAIL; 16 unit tests; skills updated | Closes the "LLM PASS on red tests" hole; one arg + one AND-gate | ~~Judge PASS impossible with failing report~~ Done — verify on next live run |
| 4 | Go port of engine stages with parity gates (Phase 3) | Single binary, no Python setup pain, native concurrency | All 4 stages at parity; Python switchable off |
| 5 | Lessons feedback loop (Reflexion-lite) | v1 logged the same blind spot twice; cheapest quality win left on the table | The non-integer-JSON lesson appears in a fresh plan's acceptance checklist unprompted |
| 6 | Scheduled discover/eval + registry proposals | Systematizes the "explore open-source models" mission; zero human memory required | New provider model → proposal artifact within a week, no manual run |
| 7 | Hot config reload (`lf_reload`) | Kills "restart after any change" footgun | Registry/pipeline change visible without restart |
| 8 | Coder-fusion isolation ablation (experiment, not code) | The one unanswered question in the v1 data: planning vs coder-stage attribution was never separated | Pre-registered: same brief per task, cf arm vs single-coder arm, 2–3 task pairs, dual judge + tests. Outcome decides cf's v2 default |

## Keep cheap (working — freeze, don't grow)

- **Reviewer panel** as conformance/implementation-bug check only. v1 proved (3/3 ablation)
  it cannot catch design gaps. Keep the security.yaml fixture, keep 3 reviewers on Ollama,
  invest nothing further.
- **Dual-judge gate** — it works and is calibrated. Only change: the test-report AND-gate.
- **Single cross-agent skill** — one file, three deploys. Resist divergence.
- **The task bank + reference implementation (T25)** — it's the parity/eval baseline; keep
  it maintained but don't expand until v2 core lands.
- **Solo/no_fusion fast path** — the effort-scaling story depends on it; carry it into every
  new surface.

## Drop / defer (distractions, with evidence)

| Item | Evidence | Disposition |
|---|---|---|
| Coder-fusion *optimization* (new variants/merge strategies) | Unvalidated, not disproven: T16–T25 never isolated the coder stage from planning; judge data is small-n and noisy | Run the isolation ablation first (see Build #8); until it reports, keep behind flag and keep `cf-1.0` metrics. Don't build variants on an unmeasured baseline |
| Reviewer panel expansion | Ablation: 3/3 reviewers passed code with a known coverage hole; false-positive history | Frozen (see above) |
| Big-bang Go rewrite | Standard rewrite risk; prompts/config are the asset, not the Python | Phased with parity gates (04-migration-plan.md) |
| Manual model-hopping | Time spent benchmarking by hand doesn't improve the loop | Replaced by #6 scheduled evals |
| Web dashboard / UI | No user need yet; artifact files + `lf_status` cover it | Defer past v2.0 |
| New providers | Two flat-rate pools already saturate 4+3 concurrency; integration cost now buys nothing | Defer; registry stays provider-agnostic |
| Multi-tenant / hosted | Different product | Out of scope |
| Parallel worktree execution | Real loop-engineering pattern, but v1's tasks are dependency-ordered and agent applies serially; complexity high | Defer to v2.1 — design jobs so per-task parallelism can be added (no shared mutable state per task) |

## Sequencing note

Items 1–3 are deliverable with the Python brain still in place (Phase 2 proxy) — meaning the
three highest-pain fixes do not wait for the Go port to finish. If time is scarce, ship 1–3
and stop; the project is already better-engineered than most of what the loop-engineering
blogs describe.

**Team-edition overlay (see 07-team-adoption.md):** items 1–3 + skill/config packaging ARE
the team quality gate — they come first unchanged. Provider abstraction (openai-compatible +
anthropic) moves into Phase 1 de-risking since model-agnosticism is now core. Item 4's
planning-stage port and item 6's scheduled evals slip behind the team pilot; items 5–6
graduate on demand (07 §5).
