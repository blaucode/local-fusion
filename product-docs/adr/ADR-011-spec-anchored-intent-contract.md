# ADR-011: Spec-anchored intent contract — the loop refuses to run without human-owned intent

**Status:** Accepted · **Date:** 2026-07-10 · **Deciders:** Adolfo

## Context

The founding philosophy decision ([PHILOSOPHY.md](../PHILOSOPHY.md) §2–3): local-fusion is
deliberately not a fully autonomous loop. Every build run must trace to human-owned intent —
a PRD for features, an approved brief for fixes, a standing charter for chore classes.
Today this lives only in convention: `lf_plan` accepts any `request` string. Nothing stops
a goal-free "improve the code" run, and the judges' requirements scores are only meaningful
relative to a real spec.

## Decision

`lf_plan` gains a required **`intent` attestation** (same pattern as `git_state`, ADR-004):

```
intent: {
  tier: "feature" | "fix" | "chore",
  ref:  "<PRD path/URL, issue link, or charter id>",
  approved_by: "<human identifier>",
  drafted_by: "human" | "agent"          # authorship is free; ownership is not
}
```

Rules: (1) missing/incomplete intent → refusal with a message naming the three tiers;
(2) `tier: feature` additionally expects the request to reference the PRD/ADR content the
agent gathered into context; (3) the attestation is recorded in `request.md` and the
manifest — fabrication is auditable, same trust boundary as `git_state` and `test_report`;
(4) charters are versioned artifacts in the store; a chore run references a charter id and
the engine checks it exists and is not expired. The **discovery loop** (P2) may *draft*
intent artifacts into a proposals inbox; nothing it drafts is runnable until a human
approval mark exists — proposal + approval, the same mechanism as registry updates and
lessons.

## Options Considered

### A: Convention only (skill instructs, engine doesn't check) — v1 status quo; erodes under
exactly the pressure it exists for (a hurried user skipping the spec). Rejected.
### B: Hard content validation (engine parses/validates the PRD itself) — the server can't
read repos (ADR-004) and "is this a real PRD?" is not machine-checkable without becoming a
bureaucracy engine. Rejected.
### C: Attestation + audit trail *(chosen)* — cheap (one argument, one refusal path),
enforces the *act* of ownership, leaves judgment of intent quality to humans and to the
plan stage's deliberation (which will surface a hollow spec's ambiguities anyway).

## Trade-off Analysis

The gate we need is behavioral, not semantic: force the human decision to exist and be
recorded. Attestation achieves that at near-zero cost. A false attestation defeats it — but
that person is also applying the code; local-fusion consistently trusts the agent/human at
that boundary and makes everything auditable after the fact.

## Consequences

- Easier: judges score against real specs; every artifact traces to an owner; the "always a
  PRD and ADRs" guarantee is a contract, not a hope; charters make chore automation possible
  without per-item ceremony.
- Harder: one more skill responsibility (gather/attach intent before plan); charter storage
  and expiry are new small store features; teams must actually write charters for chores.
- Revisit: intent-tier defaults per repo config (R9) once pilots show which tier dominates.

## Amendment (2026-07-10): haft DecisionRecords as first-class intent refs

[haft](https://github.com/m0n0x41d/haft)'s Transformer Mandate (agents frame and compare;
only the human principal records the binding decision — `h-decide` is manual-only) is this
ADR's principle implemented independently. Therefore a haft DRR reference
(`intent.ref: "haft:dec-YYYYMMDD-..."`) satisfies the feature/fix tiers with the strongest
available ownership evidence: invariants, claims, and a human binding act. Integration is
**skill-side only** — the server never reads `.haft/` (ADR-004); the skill passes DRR
content in context and, post-verdict, attaches gate evidence back to the decision via the
haft MCP. Optional, degrades to no-op, experimental until measured (PRD R14).

## Action Items
1. [ ] Add `intent` to `lf_plan` contract (ARCHITECTURE tool table updated in same commit)
2. [ ] Charter artifact type + expiry in `internal/store` (M2)
3. [ ] Skill: intent-gathering step before `lf_plan`; refusal message UX
4. [ ] PRD R13 added (same commit)
