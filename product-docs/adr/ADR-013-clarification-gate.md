# ADR-013: Pre-plan clarification gate — resolve ambiguity before spending deliberation

**Status:** Accepted (skill-side now; engine tool design-first) · **Date:** 2026-07-16
**Deciders:** Adolfo · **Prior art:** github/spec-kit `/clarify`.

## Context

The plan stage takes the request + context and deliberates. If the request is
underspecified, the deliberation *guesses* — and a wrong guess is expensive: it propagates
through haft, the coder, the reviewer, and the judge before anyone notices. The first live
gated run (2026-07-16) showed exactly this: the coder guessed the auth scheme wrong from an
ambiguous brief, costing a full coder→review→judge cycle before the FAIL. spec-kit inserts a
`/clarify` step — structured questions answered by the human — *before* planning, precisely
to stop ambiguity from becoming expensive downstream.

This is fully aligned with the philosophy: never run on thin intent; the human owns the
answers to what's ambiguous (ADR-011). It is cheap insurance against the most expensive
failure class (confident work on a misread request).

## Decision

Add a **clarification gate before `lf_plan`**, in two stages of ambition:

1. **Now — skill-side (shipped with this ADR).** `SKILL.md` gains an explicit step between
   context-gathering and planning: the agent surfaces the underspecified points as a short,
   numbered list of questions and gets the human's answers, folding them into the `request`
   and `context` before submitting `lf_plan`. Zero engine change; immediate value; the agent
   is already the right place to ask (it has the repo and the conversation).

2. **Later — an optional `lf_clarify` tool (design-first, deferred).** A synchronous tool
   that runs one TL model over `(request, context)` and returns a structured list of
   ambiguities/questions (no code, no commitment) for the agent to put to the human. It makes
   clarification model-assisted rather than relying solely on the agent's judgment, and gives
   a consistent artifact (`clarify.md`) in the trail. Deferred until the skill-side version
   shows whether the model assist is worth a stage — and its prompt would be new frozen data
   (ADR-008), recorded and parity-gated like the others.

## Options Considered

### A: Do nothing — accept that ambiguity is caught late by the reviewer/judge. Rejected: the
live run showed the cost is a full wasted build cycle, and the judge catches *symptoms* not
the root ambiguity.
### B: Force a rigid questionnaire before every plan — friction on already-clear requests.
Rejected: clarification is conditional; a clear request needs none.
### C: Skill-side conditional clarify now, optional model-assisted tool later *(chosen)* —
cheapest thing that works first; escalate to a stage only if evidence says so.

## Trade-off Analysis

The skill-side gate is nearly free (a few lines of SKILL.md) and attacks a proven-expensive
failure. Keeping the tool design-first avoids inventing a stage (and a frozen prompt, and a
parity fixture) before we know it earns its keep. Consistent with ADR-011's "the gate is
behavioral, not semantic": force the *act* of resolving ambiguity, don't over-engineer it.

## Consequences

- Easier: fewer wasted build cycles on misread requests; crisper requests feed better plans
  and sharper acceptance criteria (pairs with ADR-014 — vague acceptance is hard to attest
  coverage for).
- Harder: one more skill step (conditional, skippable when the request is clear).
- Revisit: promote to `lf_clarify` if the skill-side gate proves the model assist worthwhile.

## Action Items
1. [x] `SKILL.md`: clarification step before `lf_plan` (shipped with this ADR)
2. [ ] `lf_clarify` tool + `clarify.md` artifact + frozen prompt + parity (deferred, design-first)
