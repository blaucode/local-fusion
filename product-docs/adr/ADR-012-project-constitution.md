# ADR-012: Project constitution — persistent principles injected into plan and judge

**Status:** Accepted · **Date:** 2026-07-16 · **Deciders:** Adolfo
**Implements:** the qualitative half of PRD **R9** (per-repo rubric). **Prior art:**
github/spec-kit `constitution.md` + "Constitution Check", reframed for the multi-model loop.

## Context

local-fusion anchors every *run* to human-owned intent (ADR-011) and authorizes *chore
classes* with charters. But there is no persistent, project-level statement of **principles**
— the non-negotiables a plan must honor and a judge must score against: "use the repo's auth
middleware pattern, never inline SQL, HTTP handlers return typed errors, tests required for
every endpoint." Today those live only in whatever `context` the agent happens to gather, so
they are re-discovered (or forgotten) every run. The plan deliberation and the judge have no
durable rubric. spec-kit's insight: a small, human-authored `constitution.md`, established
once and referenced by every downstream phase, makes principles durable and checkable.

This is the qualitative complement to R9 (which also wants numeric threshold/weights — those
stay deferred, Q13 discipline). The constitution is the *content* of the rubric; R9's numbers
are its tuning.

## Decision

A per-project **`constitution.md`** in the artifact store (`projects/<project_id>/constitution.md`),
human-authored (placed in the volume like `charters/` and `providers.yaml`; the server never
invents it — proposal + approval, ADR-011). When present it is injected, **parity-safely**,
at two points:

- **Plan synthesizer** — appended to the synthesis prompt so the final brief complies with
  the principles.
- **`lf_judge`** — appended to the judge prompt so REQUIREMENTS scoring measures against the
  principles (a violated principle is a defect).

Injection is **append-only and empty by default**: with no constitution the rendered prompt
is byte-identical to today, so the record/replay parity gates (ADR-010) are unaffected. The
injection wrapper text is v2-authored prompt data and lives in `prompts/` (a new
`prompts/injections.tmpl`), checksummed by the freeze check but exempt from the v1 byte-diff
(it has no v1 source) — the frozen v1 blocks (ADR-008) are never touched or paraphrased.
`lf_status` returns whether a constitution is active (transparency, like `lf_lessons`).

## Options Considered

### A: Convention only (put principles in the request/context each run) — status quo; erodes
under exactly the pressure it exists for (a hurried run gathers thin context). Rejected.
### B: Bake principles into the frozen prompts — violates ADR-008 (frozen v1 data) and makes
principles global, not per-project. Rejected.
### C: Per-project constitution injected append-only, empty-default *(chosen)* — durable,
per-project, parity-safe, and it shares an injection point with the lessons loop (§6): the
constitution is the human-authored half, distilled lessons the machine-proposed half.

## Trade-off Analysis

Cost: one store read, two append-only injection points, one v2 template file. Benefit:
principles become durable and actually consumed by both the planning brain and the gate,
instead of relying on the agent to re-gather them. Parity is preserved by construction
(empty-default append). The risk — a stale or bloated constitution — is bounded by keeping it
human-edited and small (same discipline as lessons: ≤ ~1 screen).

## Consequences

- Easier: consistent plans and judgments across runs; the "always follow X" rules stop being
  tribal knowledge; a natural home for R9 when its numbers are designed.
- Harder: teams must actually write and maintain a constitution (optional — absent = today's
  behavior); two more injection sites to keep parity-safe (covered by an empty-default test).
- Revisit: R9 numeric threshold/weights layered on top once pilot feedback exists; whether the
  lessons distiller may *propose* constitution amendments (proposal + human approval only).

## Action Items
1. [ ] `store.ReadConstitution(project_id)`; `prompts/injections.tmpl` (constitution wrappers)
2. [ ] Append-only injection in plan synthesizer + `lf_judge`; empty-default parity test
3. [ ] `lf_status` reports constitution active; docs (configuration + tools); tests
