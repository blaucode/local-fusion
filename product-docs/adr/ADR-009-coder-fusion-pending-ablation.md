# ADR-009: Coder-fusion — port as-is, decide by ablation, never optimize unmeasured

**Status:** Accepted (the *process*; the coder-fusion default itself is deliberately undecided)
**Date:** 2026-07-09 · **Deciders:** Adolfo (default reversal is his call)

## Context

Coder-fusion (two coders → evaluator → lead merge) is ON by default in v1. Its epistemic
status is **unvalidated, not disproven**: every measured fusion win compared solo vs *full*
fusion — the coder stage was never isolated from planning, and the attribution "wins trace
to planning" is itself an inference from small-n, noisy-judge data. It triples
implementation cost/latency per task. Team pilots must not inherit an unmeasured 3× cost,
and the port must not inherit unmeasured complexity as a quality assumption.

## Decision

Three rules: (1) **Port it last and verbatim** (M3 final stage) — no improvements, no
variants, behind the same flag as v1. (2) **Run the isolation ablation** before any default
decision: same synthesized brief per task, coder-fusion arm vs single-coder arm, 2–3
pre-registered task pairs, dual judge + test gate, stopping criteria frozen up front
(v1's pre-registration discipline). (3) **The ablation decides**: no delta → default OFF
(latency/cost win); a win → stays ON and only then are variants worth exploring. Until it
reports, v1's ON default stands in research profiles; the **team pipeline profile ships
with `solo` coder** regardless (pilot cost/latency), which does not prejudge the ablation.

## Options Considered

### A: Default OFF now — reverses a deliberate v1 decision on inconclusive evidence; the
same over-claim already corrected once in this project's planning. Rejected.
### B: Optimize it (new merge strategies/coder pairs) — investment on an unmeasured
baseline. Rejected.
### C: Freeze, measure, then decide *(chosen)*.

## Trade-off Analysis

The ablation is cheap — planning (the expensive stage) is shared across arms, and the
harness (task bank, dual judge, metrics schema) already exists. Deciding by experiment
costs a weekend; deciding by opinion costs either 3× on every team task or a silently
discarded quality mechanism.

## Consequences

- Easier: honest team pitch ("every stage is either measured or flagged experimental");
  port scope for M3 shrinks (fusion path is verbatim, last, low priority).
- Harder: carrying the flag + `cf-1.0` metrics through the port; resisting the temptation
  to "just improve it while porting" (CI prompt-freeze helps, ADR-008).
- Revisit: immediately upon ablation results — amend this ADR with the data either way.

## Amendment (2026-07-09, external review): instrument prerequisite

The ablation measures *quality*, so judges are unavoidable here (unlike port parity —
ADR-010). Prerequisite before running: **either** a second validated judge (discrimination
eval ≥ separation threshold on the T25/T22 pair — note mistral-large-3, nemotron-3-super,
minimax-m3 already failed; candidates must come from new evals) **or** a pre-registered
single-judge design that compensates: `gemma4-31b` only, more task pairs (≥4), test-gate
outcomes as the objective anchor, and a declared minimum effect size larger than measured
single-judge noise. An ablation run on a silently-degraded panel is invalid by definition —
the harness must fail loudly if a judge drops out.

## Action Items
1. [ ] Pre-register the ablation spec (hypotheses, tasks, stopping criteria) before running
2. [ ] Run on v1 Python (no need to wait for the port) — 2–3 task pairs
3. [ ] Amend this ADR with results; set the v2 default accordingly
