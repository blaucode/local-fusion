# ADR-010: Port parity is verified deterministically (record/replay), not by judge scores

**Status:** Accepted · **Date:** 2026-07-09 · **Deciders:** Adolfo
**Supersedes:** the "±0.5 dual-judge avg" parity criterion in PROJECT-PLAN M3 / PRD R7 (both amended)

## Context

External review (2026-07-09) correctly found that the M3 parity gate leaned on the judge
bench: only one judge is validated (`gemma4-31b`), the second is flaky (panel silently
degrades), and measured judge noise is 0.3–2.0 — a ±0.5 parity band sits *inside* the noise.
The load-bearing validation for the whole migration used an instrument documented as
unreliable. Root insight: **port fidelity and output quality are different questions.**
LLM outputs are non-deterministic, so even Python-vs-Python runs wouldn't score identically —
judge-based parity was never sound for fidelity.

## Decision

Parity for each ported stage is verified **without any LLM in the loop**:

1. **Request parity (record/replay).** A harness records every provider request the Python
   engine emits for fixed inputs (T25 reference + synthetic cases). The Go engine, given the
   same inputs, must emit **semantically identical requests** (same model, messages after
   template rendering, max_tokens, ordering/fan-out structure; headers/transport may differ).
2. **Response-processing parity.** Both engines consume identical **canned responses** (fake
   provider) and must produce byte-comparable artifacts: manifest mutations, parsed scores,
   verdict/review/plan files, metrics records, degradation behavior on injected failures
   (timeouts, malformed output, dropouts).
3. **Live smoke (advisory, not a gate).** One real run per stage; dual-judge scores recorded
   and expected within historical noise — flagged for investigation, never a pass/fail
   criterion.

A second validated judge is **removed from the migration's critical path** and re-scoped to
where judges genuinely measure quality: the coder-fusion ablation (ADR-009 prerequisite) and
production gate robustness (v1 doc 12 Part 3).

## Options Considered

### A: Dual-judge ±0.5 band (original) — inside instrument noise; degrades silently to n=1. Rejected.
### B: Second validated judge first, then A — blocks migration on an unsolved search problem
(mistral/nemotron/minimax already failed calibration); still noise-limited even with n=2. Rejected for fidelity; kept for ablation.
### C: Deterministic record/replay + advisory live smoke *(chosen)* — exact, CI-able, free of
the judge bench; tests exactly what a port can break (request construction, parsing, control
flow) and nothing it can't (model behavior).

## Consequences

- Easier: parity in CI per PR (fake provider); porting confidence independent of judge
  bench progress; the harness doubles as the integration-test fixture (PROJECT-PLAN testing
  strategy).
- Harder: build the record/replay harness (M2 scope, ~small: the engine has one network
  choke point by design); template-rendering equivalence needs canonicalization rules
  (whitespace policy stated in the harness).
- Revisit: if request semantics legitimately diverge (e.g. Anthropic client), parity is
  defined per provider-client with shared canonical form.

## Action Items
1. [ ] M2: recording mode in Python v1 (`LF_RECORD=dir`) + replay harness in Go CI
2. [ ] Amend PROJECT-PLAN M3 exit gates + PRD R7 (done in same commit)
3. [ ] ADR-009 gains explicit prerequisite: second validated judge (or pre-registered
   single-judge + test-anchor design) before the ablation runs
