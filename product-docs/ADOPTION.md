# local-fusion v2.0 — Adoption & Distribution

> Goal: a **pragmatic loop-engineering tool anyone can adopt without the author in the
> room** — a solo engineer with a coding agent, a team standardizing a quality gate, or
> anyone in between. Design center: "someone installs it in 15 minutes, gets value on the
> first feature, and can't hurt themselves with it."
> Created: 2026-07-08 · Generalized beyond team-only: 2026-07-10

---

## 1. The three decisions (settled)

| Decision | Choice | Why |
|---|---|---|
| Form factor | Evolve local-fusion v2 (Go server + per-repo skill) | Reuses everything; new adopters are a deployment target, not a second product |
| Models | **Model-agnostic** | Adopters use what they have (Claude, GPT, open-weight). The flat-rate open-model fusion becomes an optional *cost profile*, not the premise |
| Entry scope | **Quality gate only** | The most proven, least contested, easiest-to-adopt slice: judged verdict + test evidence + paper trail on every agent-built feature |

**Distribution channels:** the Docker image + one MCP config block (the server), the
per-repo skill file committed like CI config (spreads via PR), and the open repo itself.
A solo adopter needs only the first two; the team path below adds the pilot/architect
motions on top — it is one adoption route, not the definition of the product.

## 2. What "the tool" is, concretely

**The pitch to an engineer:** *"Your agent builds the feature like it does today. local-fusion
makes it prove the work: tests attached, independent judges scoring req/sec/maint, a written
verdict, and a paper trail your architect can read. PASS ≥ 8.0 with green tests or it
doesn't ship."*

**The pitch to an architect:** *"Every agent-built change arrives with an ADR-shaped brief,
a review report, and a calibrated verdict — instead of a 400-line diff and vibes."*

Components the team touches:

1. **The server** — one shared container (or per-dev `docker run`). Zero Python, zero deps.
2. **The skill** — one file dropped into the repo (`.claude/`/`.cursor/`/`.cline/skills/`),
   checked into git like CI config. This is the distribution mechanism: adopting the tool =
   merging a PR.
3. **The gate contract** — `lf_judge(changed_files, test_report)`; PASS ⇔ tests green AND
   score ≥ threshold. Threshold and judge models set in repo-level config, not per-engineer.
4. **The artifacts** — `local-fusion/<slug>/` in-repo trail (agent-materialized), reviewable
   in the PR itself.

Explicitly **not** in team v1: planning deliberation, coder-fusion, reviewer panels,
scheduled evals. They stay in your research profile and graduate individually (see §5).

## 3. Model-agnostic mechanics (the one real design change)

The v1 registry already abstracts providers; team edition needs one more step:

- **Provider types**: `openai-compatible` (covers Featherless, Ollama, OpenAI, most
  gateways) + `anthropic`. Two client implementations, everything is config.
- **Judge selection rule stays**: two judges, *different model families*, because judge
  diversity — not open weights — is what the dual-judge design actually requires.
- **Cost profiles** ship as presets: `flat-rate` (your Featherless/Ollama setup), `byok`
  (team's existing API keys), `mixed`. A profile is just a providers.yaml variant.
- The eval harness (`discover`/`eval`, promotion ≥ 8.5) is unchanged — it's how a team
  qualifies *their* models as judges instead of trusting your bench.

## 4. Adoption path (how it spreads without you)

1. **Week 0 — you**: run the gate on your own repos until the skill + config feel boring.
2. **Pilot — one engineer, one repo**: PR adds the skill + config; they run 2–3 features.
   Success metric: they keep using it when nobody's watching.
3. **Architect loop-in**: architects read verdicts/briefs in PRs for two weeks. Their
   feedback tunes the rubric (req/sec/maint weights, threshold) — this is where the tool
   earns the architecture team, and rubric-tuning is *their* ownership hook.
4. **Team default**: gate documented in the team's definition-of-done. Only now consider
   graduating stage 2 (planning).

Each step has an exit: if the pilot engineer drops it, find out why before writing more code.

## 5. Graduation ladder (features earn their way in)

| Stage | Graduates when |
|---|---|
| 1. Quality gate | — (team v1) |
| 2. Plan deliberation (haft → ADR → acceptance) | ≥2 engineers ask "can it help *before* coding?" AND gate adoption is sticky |
| 3. Reviewer panel | Architects want conformance checks between apply and judge (keep it framed as impl-bugs-only) |
| 4. Coder-fusion | Only if the isolation ablation (ADR-009) shows a win. Never before |
| 5. Lessons/memory, scheduled evals | Gate has ≥1 month of metrics.jsonl across the team to distill from |
| 6. Discovery loop (drafts briefs/issues into a proposals inbox) | Charters exist and stage-2 planning is adopted; drafts are never runnable without human approval (ADR-011) |

### Autonomy levels (orthogonal to the ladder — per loop, per repo)

Borrowed from the loop-engineering rollout discipline (report-only → assisted → unattended):

| Level | Meaning | local-fusion mapping |
|---|---|---|
| A1 — Report-only | Loop observes and proposes; touches nothing | Discovery loop drafting briefs; judge run as advisory on existing PRs |
| A2 — Assisted | Loop acts, human approves each unit | **The team gate (default, and the ceiling for v2.0):** agent builds, human approves intent (ADR-011) and merges on evidence |
| A3 — Unattended within a charter | Loop acts without per-item approval, bounded by a standing human-approved charter | Chore-class runs only (e.g. dep bumps); requires charters + budgets + the full hard-stop set. Post-v2.0 |

Every loop starts at A1 in a new repo. Nothing skips a level. Feature work never reaches A3
— that is the spec-anchored line ([PHILOSOPHY.md](./PHILOSOPHY.md) §2).

## 6. Anti-over-engineering guardrails (write these on the wall)

1. **No feature ships before a team member asks for it twice.** The graduation ladder is
   demand-driven, not roadmap-driven.
2. **One binary, one skill file, one config.** If setup needs a wiki page, it's too complex.
3. **The agent stays the hands.** No autonomous code application, no git access from the
   server — the invariant is also the security story that makes team adoption approvable.
4. **Deterministic checks outrank models, always.** Tests/linters gate; judges score. Never
   invert it.
5. **Conventions become guarantees only when they break.** Don't build enforcement for
   problems nobody has hit (v1's judge-retry ledger earned its place; most things won't).
6. **Measure adoption like you measured fusion.** metrics.jsonl gains `user`/`repo` fields;
   a tool nobody runs is a null result — record it honestly and act on it.
7. **Your research profile and the team profile share one codebase, differing only in
   config.** The moment they fork, you're maintaining two products.

## 7. What this changes in the v2.0 build order

Roadmap items 1–3 (async server, budgets, test-report-gated judging) are unchanged — they
*are* the team gate. Additions/reorderings:

- **Provider abstraction (openai-compatible + anthropic) moves into Phase 1** de-risking —
  it's now core, not future-proofing.
- **Skill packaging + repo config** become a first-class deliverable (the distribution
  mechanism).
- Planning-stage Go port (migration Phase 3, item 4) can slip behind the pilot — the gate
  doesn't need it.
- Scheduled evals (roadmap #6) deprioritized to graduation stage 5.

## 8. Open questions specific to team edition

- **Q11 — Where does the shared server run?** Per-dev localhost container (zero ops, N
  configs) vs one internal host (one config, needs the bearer-token story from Q6).
  *Leaning: per-dev first; shared host when someone complains.*
- **Q12 — Gate enforcement point?** Skill-level (agent won't proceed) vs CI-level (PR check
  reads `verdict.md`). *Leaning: skill-level for v1; CI check is a natural stage-2 ask and a
  great architect hook.*
- **Q13 — Rubric ownership?** Architects own req/sec/maint weights + threshold per repo.
  Needs a tiny `local-fusion.yaml` in-repo config — decide its schema before the pilot.
