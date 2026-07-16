# ADR-014: Acceptance-coverage gate — every acceptance criterion must be covered

**Status:** Accepted · **Date:** 2026-07-16 · **Deciders:** Adolfo
**Extends:** ADR-006 (deterministic test gate). **Prior art:** github/spec-kit `/analyze`
(cross-artifact coverage), reframed as a deterministic attestation in local-fusion's model.

## Context

The plan synthesizer emits `acceptance.md` — "a checklist of what *done* looks like, one
item per line." Today nothing checks that the implementation actually *covers* each item.
The deterministic gate (ADR-006) proves tests pass; the dual judge scores req/sec/maint.
But "green tests + high scores" can still miss a specific acceptance criterion — exactly
what the first live gated run surfaced (2026-07-16): tests were green, the judges averaged
7.83 → FAIL, and the missed item was an acceptance criterion the judges happened to catch by
opinion. Catching a missing requirement should be a *guarantee*, not a lucky judge.

Loop-engineering principle (same as ADR-006): deterministic verification is the reward
signal; model judgment is for what can't be mechanically checked. "Did we build every thing
the brief asked for?" is mechanically checkable *if* the agent attests which evidence covers
each criterion — the same trust boundary as `test_report` (ADR-004/006) and `git_state`.

## Decision

`lf_judge` gains an optional **`acceptance_coverage`** argument: an ordered list of evidence
strings, one per acceptance criterion (in the order the criteria appear in `acceptance.md`).
The engine parses `acceptance.md` into an ordered criteria list and the gate extends ADR-006:

> **PASS ⇔ `exit_code == 0` AND dual-judge avg ≥ threshold AND every acceptance criterion is
> covered.**

Coverage rules, deterministic and applied *after* aggregation (like the test gate):

1. **No parseable criteria** (empty/prose `acceptance.md`) → the coverage gate is inert
   (backward-compatible; parity fixtures with empty acceptance are unaffected).
2. **Criteria exist, no `acceptance_coverage`** → **FAIL**, `gate_reason` lists the N criteria
   that must be attested. Coverage is required-in-practice whenever the brief has criteria —
   the convention becomes a guarantee (same move as ADR-007's judge ledger).
3. **Criteria exist, coverage incomplete** (fewer entries than criteria, or any blank entry)
   → **FAIL**, `gate_reason` names the uncovered criteria by text.
4. **Full coverage** → the coverage gate passes; the verdict is decided by tests + scores.

A criterion is any non-blank `acceptance.md` line matching a checklist/bullet/numbered form
(`- item`, `* item`, `- [ ] item`, `1. item`) — documented and stable. `acceptance_coverage[i]`
covers `criteria[i]`; the judge response and `lf_status` return the parsed, numbered criteria
so the agent knows exactly what to attest. `verdict.md` gains a COVERAGE section.

## Options Considered

### A: Model-based coverage judge — a stage scores "is each criterion covered?"
Advisory (a model), not deterministic; adds cost and a new SPOF. Rejected — the whole point
is a mechanical guarantee, not another opinion.

### B: Structural heuristic (e.g. #tests ≥ #criteria) — crude, gameable, false confidence. Rejected.

### C: Coverage attestation, deterministically gated *(chosen)* — the agent maps each criterion
to its evidence; the engine enforces completeness. Mirrors `test_report` exactly: cheap, honest,
auditable, and it makes "we built what was asked" impossible to skip by construction.

## Trade-off Analysis

Cost: one optional argument, one parser, one post-aggregation conditional — the same shape
as the test gate. Benefit: the gate's second-worst failure (green tests, lenient scores, but
a requirement silently dropped) becomes structurally caught. A false attestation defeats it,
but that agent is also applying the code and running the tests — local-fusion trusts the
agent at that boundary and records the attestation in `verdict.md` for audit. The gate stays
parity-safe because no model prompt changes: coverage is post-hoc and inert when absent.

## Consequences

- Easier: trusting a PASS end-to-end (tests + design + completeness); the acceptance checklist
  becomes load-bearing instead of decorative.
- Harder: the agent must attest coverage (skill mandates it, like the test report); a brief
  with vague acceptance items produces vague attestations — which is a useful pressure toward
  crisp, checkable criteria (and pairs with ADR-013's clarify gate).
- Revisit: linter/static-analysis reports as further deterministic inputs (ADR-006's noted
  extension); auto-deriving coverage from test names once a mapping convention exists.

## Action Items
1. [ ] `internal/engine/judge`: parse `acceptance.md`, `ApplyAcceptanceGate` after the test gate
2. [ ] `lf_judge` `acceptance_coverage` arg; parsed criteria + coverage status in the response
3. [ ] `verdict.md` COVERAGE section; skill mandates attestation; docs + tests
