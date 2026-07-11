# PRD — local-fusion v2

**Status:** Draft for review · **Owner:** Adolfo (PM/Arch) · **Date:** 2026-07-09
**Companion docs:** [PROJECT-PLAN.md](./PROJECT-PLAN.md) · [adr/](./adr/) · deep design in [ARCHITECTURE.md](./ARCHITECTURE.md)

---

## 1. Problem Statement

The leverage point in working with coding agents has moved from writing prompts to designing
the loops that prompt them (Steinberger, Cherny, Osmani — June 2026). That shift created a
new failure surface faster than it created tooling to manage it. Three problems compound:

**Agent output arrives without evidence.** Agents now write most feature code, but it lands
as large diffs with no independent proof of quality. Engineers babysit sessions; architects
reverse-engineer intent from code and sign off on vibes. The loop-engineering literature is
unanimous that verification is the piece practitioners obsess over and tooling skips — "a
loop that writes code without a feedback mechanism is a machine for generating confident
mistakes at scale" — yet the ecosystem keeps shipping chore loops (triage bots, PR
babysitters, dependency sweepers) while the **verification loop** remains everyone's
unbuilt homework. LLM-as-judge alone doesn't solve it: judges are noisy, gameable, and
degrade silently; without deterministic anchors and calibration they add confidence, not
trust.

**Naive loops fail in known, expensive ways.** The production record of 2026 is explicit:
loops that don't halt (the AutoGPT lesson relearned at scale), token bills that ended in
per-engineer spending caps at large companies, reward hacking against checkable proxies, and
two debts that grow with loop speed — *comprehension debt* (shipping code nobody read) and
*cognitive surrender* (letting the loop decide what's worth building). Autonomy without
spec-anchoring doesn't just miss edge cases; it erodes the team's ownership of its own
system.

**The proven fix is operationally unshippable.** local-fusion v1 built and measured the
missing verification loop — deliberated planning plus a calibrated multi-model gate, worth
+0.8 to +4.0 judge points on hard, underspecified tasks (nulls on easy ones, kept in the
record). But it is a one-person research rig: minutes-long synchronous MCP calls that fight
client timeouts, a Python setup that breaks on interpreter mismatches, host filesystem/git
coupling, and termination rules that live in prompt conventions instead of the engine.

The cost of not solving this: the team adopts loop tooling anyway — agents are already
writing the code — but gets the chore-loop half without the verification half, ungated and
unanchored. v2's job is to make the measured quality loop deployable, enforceable, and
**spec-anchored**: every run must trace to human-owned intent — a PRD, an approved brief, or
a charter (see [PHILOSOPHY.md](./PHILOSOPHY.md); doctrine in full, including why 100%
autonomy is a non-goal).

## 2. Goals

1. **Zero-babysit operation** — a full gated feature run completes with no timeout
   configuration in any agent, and long stages survive agent disconnects. *(Measure: pilot
   runs with default MCP client config, 0 timeout failures.)*
2. **15-minute team adoption** — a teammate goes from nothing to a first gated feature in
   ≤15 min with `docker run` + one MCP config block + one skill file. *(Measure: timed
   first-run by 2 pilot engineers, no help from Adolfo.)*
3. **Trustworthy gate by construction** — a PASS is impossible with red tests, a third judge
   attempt is refused by the engine, and every run has an artifact trail. *(Measure:
   kill-switch and gate tests in CI; 100% of pilot runs produce verdict artifacts.)*
4. **Model-agnostic** — works with the team's existing models/keys (OpenAI-compatible +
   Anthropic), with the flat-rate open-weight setup as a cost profile, not a prerequisite.
   *(Measure: one pilot runs entirely on non-Featherless providers.)*
5. **v1 fidelity, deterministically proven** — ported stages emit semantically identical
   provider requests and byte-comparable artifacts vs the Python engine (ADR-010) before
   Python is switched off. *(Measure: record/replay parity gates in PROJECT-PLAN M3; live
   judge scores advisory only.)*

## 3. Non-Goals

- **Autonomous code application.** The agent (human-supervised) applies all files and runs
  git; the server never touches a repo. This is the safety story — not a missing feature.
- **Coder-fusion optimization.** Unvalidated vs planning (never isolated in v1 experiments);
  frozen behind a flag until the ablation (ADR-009) reports.
- **Web dashboard / UI.** Artifacts + `lf_status` cover observability needs at this scale.
- **Multi-tenant/SaaS.** Single team, localhost-or-LAN deployment; a bearer token is the
  entire auth model (ADR-002).
- **New pipeline stages.** Team v1 = quality gate; planning/review graduate on demand
  (ADOPTION.md §5). Prevents building features nobody asked for.

## 4. Users & User Stories

**P1 — Software engineer (primary):**
- As an engineer, I want my agent to submit long planning/judging work as background jobs so
  that my session never times out or blocks.
- As an engineer, I want the gate to demand my real test results so that a PASS means
  something when I open the PR.
- As an engineer, I want setup to be one container and one config block so that I don't
  maintain a Python toolchain for a side tool.
- As an engineer, I want a failed second judge attempt to stop the loop and tell me why, so
  the agent doesn't burn an hour circling.

**P2 — Software architect:**
- As an architect, I want every agent-built feature to arrive with a brief, review findings,
  and a two-judge verdict so that I review evidence, not vibes.
- As an architect, I want to tune the rubric/threshold per repo (post-pilot, P1 req) so the
  gate reflects our standards, not defaults.

**P3 — Operator (Adolfo):**
- As the operator, I want stage-level progress on running jobs so I can see where a 20-minute
  plan is stuck.
- As the operator, I want config hot-reload so a model swap doesn't require restarting a
  server others are using.
- As the operator, I want every verdict logged to metrics so adoption and quality claims stay
  evidence-based.

## 4a. Core Scenarios

- **S1 — Gated feature build (the main loop).** Agent gathers context → `lf_plan` (job) →
  per task: `lf_coder_fusion` or agent implements → agent applies + tests → `lf_review` →
  `lf_judge` with test report → PASS or one engine-tracked retry → next task.
- **S2 — Overnight planning.** Three feature requests submitted as plan jobs before logging
  off; three deliberated briefs (ADR + plan + acceptance) ready in the morning. Planning is
  the proven-value stage — unattended *planning* pays; unattended *coding* is a non-goal.
- **S3 — Continuous model bench** *(P2)*: scheduled discover/eval writes registry-update
  proposal artifacts; human approves.
- **S4 — Cross-project lessons** *(P2)*: recurring judge findings distilled into per-stack
  lessons injected at plan time — ships only with a validation design (see Appendix).

## 5. Requirements

### P0 — Must have (gate + operations core)

| # | Requirement | Acceptance criteria |
|---|---|---|
| R1 | **Async job model** for `lf_plan`/`lf_coder_fusion`: submit → `job_id`; `lf_job` polls status/progress/result; `lf_cancel` | Given a plan taking 20 min, when submitted, then the submit call returns <2s and `lf_job` reports stage-level progress; agent crash + reconnect can resume polling |
| R2 | **Streamable HTTP MCP transport** + Docker image; **stdio kept** for local/back-compat; static bearer token when bound beyond localhost | All 3 agents (Claude Code, Cline, Cursor) connect via URL config; `docker run` + env-file works on a Python-free machine |
| R3 | **Deterministic test gate** (parity with v1's `apply_test_gate`): PASS ⇔ tests green AND avg ≥ threshold; malformed report = explicit error | Given exit_code≠0, when judged, then verdict FAIL regardless of scores (CI test) |
| R4 | **Engine-enforced termination**: per-job wall-clock/step budgets, judge-retry ledger (3rd attempt refused → escalate), no-progress detection | Kill-switch test: 5-min budget job provably stops ≤5 min with partial artifacts + `budget_exhausted` status |
| R5 | **Filesystem-free server**: all artifacts returned as data + stored in engine volume; agent materializes in-repo copies; git ops move to skill with `git_state` attestation | Server runs in a container with no repo mount; `lf_plan` refuses without clean-tree attestation |
| R6 | **Model-agnostic providers**: `openai-compatible` + `anthropic` client types; registry/pipelines schema preserved from v1 | One full gated run on non-Featherless models; providers.yaml from v1 loads unmodified |
| R7 | **v1 parity for ported stages** (judge → review → coder-solo → plan), each behind an engine switch | Per-stage, **deterministic** (ADR-010): provider requests semantically identical under record/replay; artifacts byte-comparable on canned responses; live dual-judge smoke advisory only |
| R13 | **Spec-anchored intent contract** (ADR-011): `lf_plan` requires an `intent` attestation — tier (feature/fix/chore), reference, human approver | Given no/incomplete intent, `lf_plan` refuses naming the three tiers; attestation recorded in request.md + manifest; chore tier validates charter id exists and is unexpired |
| R15 | **User documentation shipped with the product** (`docs/` in this repo, docs-as-code; R14 retired). Minimum set: quickstart (install → first gated feature); *How it works* (loop, stages, artifacts, gate semantics — for users, not implementers); *Benefits* (the evidence: what the gate catches, v1 experiment results, when fusion pays); configuration reference (annotated `providers.yaml` examples per cost profile, budgets, thresholds); MCP setup per agent (Claude Code / Cline / Cursor, HTTP + stdio, Docker run, token); tool reference (every `lf_*` tool, args, errors); troubleshooting/FAQ | Docs exist for every shipped feature at its milestone (DoD); a person who didn't build it completes setup + first gated run using only `docs/` (this IS Goal 2's 15-minute test — no help from the author); every config key documented with a working example; every engine refusal/error message names its docs section |

### P1 — Nice to have (fast follows)

- **R8 Config hot-reload** (`lf_reload`) — kills v1's restart-after-any-change footgun.
- **R12 Per-run cost visibility** — token counts × configured price per model in every job
  result and metrics record; BYOK profiles get a per-run cost line. The flat-rate "diversity
  is free" premise does NOT transfer to metered keys; a budget owner must see the bill per
  run before the pilot widens. (External review finding #5.)
- **R9 Per-repo rubric config** (threshold, req/sec/maint weights) owned by architects —
  schema designed only after pilot feedback (Q13 discipline).
- **R10 Stage-granular progress narration** in the skill ("task 2/4: TL panel 1/3").
- **R11 Chunked review/judge** (map-reduce over file groups) — lifts the ~37-file/32K ceiling.

### P2 — Future considerations (design for, don't build)

- Lessons/Reflexion feedback loop (needs ≥1 month of team metrics first).
- Scheduled model discover/eval with registry proposals.
- Parallel per-task execution (keep job state free of shared mutable per-task state now).
- CI-level gate check reading `verdict.md` (natural stage-2 architect ask).

## 6. Success Metrics

**Leading (first 30 days of pilot):** 2+ engineers complete a gated feature unaided; ≥80% of
gated runs produce a verdict without operator intervention; 0 timeout-related failures;
median setup time ≤15 min.

**Lagging (quarter):** gate adopted as definition-of-done in ≥1 team repo; ≥1 documented
catch (gate FAIL that prevented a real defect reaching review); pilot retention — engineers
still using it in week 6 without prompting (a dropped pilot triggers a written why).

### Pilot-widening preconditions (beyond software)

Before the pilot goes past 1–2 engineers: the **data-governance sign-off** must exist
([DATA-GOVERNANCE.md](./DATA-GOVERNANCE.md) §4
checklist — provider retention terms pinned, profile-per-repo rule documented) and **R12
cost visibility** must be live for any BYOK profile in use.

## 7. Open Questions

Tracked with owners in [OPEN-QUESTIONS.md](./OPEN-QUESTIONS.md). Blocking for M1:
**Q1** (Go `net/http` vs Cloudflare/Featherless — engineering, live spike) and **Q3** (Go MCP
SDK Streamable-HTTP maturity across all 3 agents — engineering, live spike). Non-blocking:
Q8 (32K budgets — data-first), Q13 (rubric schema — after pilot feedback).

## 8. Timeline & Phasing

No hard external deadline; sequencing is risk-driven — see [PROJECT-PLAN.md](./PROJECT-PLAN.md).
M1 spikes (Q1/Q3) gate everything: if either fails, fallbacks (curl shim / alternative SDK /
stdio-first) are decided before M2 starts, not discovered mid-build. The team pilot starts on
M2 (Go shell + Python brain) — value ships before the port finishes.

## Appendix — Scope decisions (evidence-backed, from v1 experiments)

**Keep, don't grow:** reviewer panel stays a conformance/implementation-bug check (v1
ablation: 3/3 reviewers miss planning gaps — design-gap detection belongs to the plan stage);
dual-judge gate unchanged except the test-report AND-gate (ADR-006, shipped in v1
2026-07-08); one cross-agent skill file; solo/`no_fusion` fast path carried into every
surface (effort scales to difficulty).

**Deliberately deferred, with reasons:** coder-fusion variants (ADR-009 — ablation first);
lessons/Reflexion loop (ships only with a lesson-on/off validation design, same standard as
coder-fusion); web dashboard (no user need; artifacts + `lf_status` suffice); new providers
beyond the two client types (ADR-008); multi-tenant/hosted (different product); parallel
worktree execution (v2.1 — job state designed now to permit it: no shared mutable per-task
state); chunked review/judge for large diffs (lifts the 32K/~37-file ceiling — R11).
