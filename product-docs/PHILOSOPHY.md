# Philosophy — The Spec-Anchored Loop

> local-fusion v2 is a loop-engineering product with one refusal at its core: **it will not
> run without human-owned intent.** This doc is the doctrine behind every mechanism in the
> PRD, plan, and ADRs. If a design choice ever conflicts with this doc, this doc wins or
> gets amended — never silently ignored.
> Created: 2026-07-10

---

## 1. Where we sit in the loop stack

The 2026 loop-engineering canon (Osmani's five primitives + memory; Cherny's "cron plus a
decision-maker"; LangChain's four stacked loops; Greyling's operational patterns) describes
a stack:

| Loop | What it does | The ecosystem | local-fusion v2 |
|---|---|---|---|
| L1 — Agent loop | Model calls tools until done | Every coding agent | **Not ours.** The agent (Claude Code / Cline / Cursor) is the hands; we plug into it |
| L2 — Verification loop | Grade output against a rubric, feed back, retry | Mostly unbuilt: chore bots dominate (triage, PR babysitters, dep sweepers) | **Our product.** Deliberated briefs + deterministic test gate + calibrated dual judge — the best-measured L2 we know of (25 experiments, nulls kept) |
| L3 — Event-driven loop | Schedules/webhooks trigger runs | Where the ecosystem plays | Plumbing, added later (scheduler exists in the architecture; graduates on demand) |
| L4 — Hill-climbing loop | Traces feed an analyzer that improves the harness | Frontier | The lessons loop — ships only with a validation design, like everything else here |

The discourse builds loops that *find and do chores*. We build the loop that *proves quality*
— the layer Cherny calls the one "the hype consistently skips and the practitioners
consistently emphasize": a loop is only as trustworthy as its ability to check its own output.

## 2. The core principle: the spec-anchored loop

**The loop's entry ticket is human-owned intent, written down.** A build run cannot start
without an intent artifact — a PRD reference, an approved brief, or a charter (§3). The
engine refuses, the same way it refuses a dirty git tree (ADR-011).

Why this is a feature and not friction:

- **Anti-cognitive-surrender by construction.** Osmani's warning: the same loop moves one
  person faster on work they understand and lets another avoid understanding entirely — "the
  loop doesn't know the difference." Ours does: the human can't stop having an opinion,
  because the opinion *is* the entry ticket.
- **Intent debt is paid up front.** An agent fills any hole in your intent with a confident
  guess. The plan stage deliberates *against a written spec*, not against a vibe.
- **The verdict means something.** Judges score req/sec/maint *against the brief*. No brief,
  no meaningful "requirements" score — spec-anchoring is what makes the gate an instrument
  instead of an opinion.

We are deliberately **not** building 100% autonomy. Cherny's stage-five world (hundreds of
agents deciding what to build from Slack and Twitter) is explicitly not this product.

## 3. Proportional intent: authorship ≠ ownership

**The loop may draft intent; it may never approve its own intent.** Machines author,
humans own. Intent scales with blast radius but is never zero:

| Tier | Work | Required intent | Who may draft it | Who must approve |
|---|---|---|---|---|
| Feature | New capability, architecture-touching | PRD reference + ADRs | Human or discovery loop | Human, explicitly |
| Fix | Bugfix, bounded change | Linked issue/brief | Often the discovery loop | Human (one-click ack is fine) |
| Chore class | Recurring bounded work (dep bumps, cleanups) | A **charter**: a standing human-approved policy authorizing the class, with constraints | Drafted once, usually together | Human, once per charter revision |

This yields a **two-loop structure**: a *discovery loop* (L3-style triage over CI failures,
judge findings, TODOs) drafts briefs into an inbox; the *build loop* executes only
human-approved ones. The pattern is already native here — registry updates and lessons both
work as **proposal + approval**. That is local-fusion's universal answer to "can the loop
feed itself?": *yes, up to but never through the approval.*

How the intent documents get authored is deliberately **outside the loop's scope**. Humans
may write them by hand or with governance tooling — e.g. [haft](https://github.com/m0n0x41d/haft),
whose Transformer Mandate (agents frame and compare; only the human principal binds) is this
same principle implemented independently, and which the author uses upstream to produce PRDs
and ADRs. The loop consumes intent documents; it does not integrate with, depend on, or care
about the tool that produced them.

## 4. Verification doctrine

1. **Deterministic checks outrank models, always.** Tests, compilers, linters are the reward
   signal; the gate is PASS ⇔ tests green AND judge avg ≥ threshold (ADR-006). No prompt
   content can flip an exit code — this is also our injection-resistant layer.
2. **Maker and checker never share a brain** — different models, different roles; applied
   even to the stop condition (independent judges decide done-ness, not the coder).
3. **Instruments must be calibrated or benched.** Judges pass a discrimination eval or they
   don't judge. A silently degraded panel invalidates the run.
4. **"Done" is a claim, not a proof.** The gate produces *evidence for human review* — it
   never auto-merges. Comprehension debt is paid by a human reading verdicts and diffs.

## 5. Operational doctrine

- **Hard stops are non-negotiable** (the production consensus): step caps, wall-clock and
  token/dollar budgets, no-progress detection, judge-retry ledger — engine-enforced, never
  prompt conventions (ADR-007). The most expensive loop is the one that doesn't halt.
- **Skills are the asset, loops are the plumbing.** Hard-won knowledge lives in reusable,
  versioned artifacts — prompts as frozen data, the skill file, charters, lessons — so every
  future run compounds instead of re-deriving.
- **Effort scales to difficulty.** Full deliberation for ambiguous work, solo fast-path for
  simple work. Fusion earns its cost only where measured (+0.8 to +4.0 on hard tasks; ~0 on
  easy ones).
- **Measure or flag.** Every stage is either backed by experiment data or explicitly marked
  experimental (coder-fusion, lessons). Null results are kept.
- **Durable state outside any conversation.** The artifact graph and metrics log are the
  loop's memory; the model forgets, the store doesn't.

## 6. The three debts we design against

| Debt | What it is | Our countermeasure |
|---|---|---|
| Intent debt | Agent guesses where you were silent | Spec-anchored entry; deliberation surfaces ambiguities *before code* |
| Comprehension debt | Shipping code nobody read | Gate evidence in the PR; human review is the merge condition, not the verdict |
| Cognitive surrender | Letting the loop think for you | Human-owned intent as precondition; proposal + approval everywhere |

## Sources

Addy Osmani, [Loop Engineering](https://addyosmani.com/blog/loop-engineering/) (primitives,
intent/comprehension debt, cognitive surrender) · Boris Cherny via
[three-stage definition](https://medium.com/mountain-movers/what-a-loop-actually-is-boris-chernys-three-stage-definition-33dd2bfe01b3)
(loop = cron + decision-maker; hard stops; skills-are-the-asset) · LangChain,
[The Art of Loop Engineering](https://www.langchain.com/blog/the-art-of-loop-engineering)
(four-loop stack, verification loop, hill-climbing) · Cobus Greyling,
[loop-engineering](https://github.com/cobusgreyling/loop-engineering) (state/budget/constraints
as repo files, phased rollout, human gate in the loop anatomy).
