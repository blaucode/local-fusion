# ADR-006: Deterministic test gate outranks LLM judges

**Status:** Accepted — **implemented in v1** (2026-07-08, `judge.py::apply_test_gate`, 16 unit tests)
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

The v1 gate was LLM-only: agents ran tests, but judges never saw results — a lenient judge
could PASS code with red tests. Worse, v1's judge bench has a single point of failure: only
`gemma4-31b` is validated (separation 3.11); `deepseek-v4-pro` is flaky and the dual panel
silently degrades to a single judge when it drops. Loop-engineering first principle:
deterministic verification is the reward signal; model judgment is for what can't be
mechanically checked.

## Decision

`lf_judge` takes a `test_report` (`{command, exit_code, summary}`, dict or JSON string).
**PASS ⇔ `exit_code == 0` AND dual-judge avg ≥ threshold.** Non-zero exit forces FAIL
regardless of scores; judges also *receive* the report (calibrates req scoring). Malformed
report → explicit error, never silently ignored. The skill mandates: never judge untested
code; never fabricate the report. v2 ports this contract unchanged and keeps `test_report`
**required in the team pipeline profile** (optional only in research profiles).

## Options Considered

### A: LLM-only gate (v1 status quo) — judge leniency/degradation can pass broken code. Rejected.
### B: Tests-only gate, drop judges — loses req/sec/maint signal and design scoring where
tests can't reach (the measured value of the judge bench). Rejected.
### C: AND-gate: deterministic first, judges second *(chosen)* — each covers the other's blind
side: tests catch what judges excuse; judges catch what tests don't cover.

## Trade-off Analysis

Cost: one argument and one conditional. Benefit: the gate's worst failure mode (confident
PASS on broken code) becomes impossible by construction rather than improbable by prompting.
Remaining exposure — green tests + lenient judge over-scoring bad *design* — is why the
second validated judge remains wanted (tracked in v1 doc 12 Part 3), but that gap is
strictly smaller than v1's.

## Consequences

- Easier: trusting a PASS; architect sign-off; CI-check graduation later (P2).
- Harder: agents must produce honest reports (attested, like ADR-004 — same trust boundary);
  repos without tests get no deterministic protection (pilot rule: gate implies a test suite).
- Revisit: add linter/static-analysis reports as additional deterministic inputs (natural
  extension, not v2.0 scope).

## Action Items
1. [x] v1 implementation + unit tests (2026-07-08)
2. [x] One live gated run verified wiring end-to-end (M2, `8105269`)
3. [x] Ported to Go with identical semantics; parity gate in CI (M2f, judge stage)
