# local-fusion v2.0 — Open Questions

> Decisions to settle before/while coding. Each has a leaning; none is frozen.
> Created: 2026-07-07

## Decision log (2026-07-08, overnight pass — Adolfo can veto any of these)

| Q | Status | Decision |
|---|---|---|
| Q1 (Go vs Cloudflare) | **OPEN — needs live test** | Cannot be decided from an armchair; Phase 1 day 1. Fallback path documented |
| Q2 (artifact home) | **DECIDED** | Engine volume canonical; agent materializes in-repo copy |
| Q3 (Go MCP SDK) | **OPEN — needs live test** | Official SDK first; verify against all 3 agents in Phase 1 |
| Q4 (jobs execution) | **DECIDED** | In-process goroutines + context cancellation; revisit only on evidence |
| Q5 (coder-fusion default) | **DECIDED** | v1 default (ON) stands until the isolation ablation reports; no variants before it |
| Q6 (server auth) | **DECIDED** | Bind 127.0.0.1 by default; optional static bearer token env var |
| Q7 (lessons size) | **DECIDED** | ≤30 lines/stack, human-editable, distiller proposes via artifact |
| Q8 (32K ceiling) | **DECIDED (minimum)** | Log input token counts per call in metrics first; budget-per-stage design later, driven by that data |
| Q9 (scheduler) | **DECIDED** | Inside the server; tiny cron loop |
| Q10 (gitops replacement) | **DECIDED** | Skill-side git + `git_state` attestation arg required by `lf_plan` |
| Q11 (server placement, team) | **DECIDED** | Per-dev localhost first; shared host only when someone complains |
| Q12 (gate enforcement point) | **DECIDED** | Skill-level for the pilot; CI check on `verdict.md` is the natural stage-2 graduation |
| Q13 (rubric config) | **DEFERRED — deliberately** | v1 doesn't read a repo config; pilot runs with fixed 8.0 threshold. Schema designed only after architect feedback exists (avoids inventing config nobody asked for) |

Pragmatic rule applied: decide everything that doesn't require new information; leave open
exactly the two questions that can only be answered by running real code against real
services (Q1, Q3).

---

## Q1 — Does Go's `net/http` pass Featherless/Cloudflare?

v1 abandoned urllib for curl (403 / error 1010). Go has a real TLS stack, but Cloudflare
fingerprints TLS (JA3), not just User-Agent.
**Leaning:** test in Phase 1 day 1. Fallback: `utls` library or a curl-exec shim behind the
provider interface. Do not design anything else around this until tested.

## Q2 — Where do artifacts canonically live?

Engine volume (queryable, survives repo deletion, cross-project lessons) vs in-repo
`local-fusion/<slug>/` (reviewable in PRs, greppable, v1 habit).
**Leaning:** engine volume is canonical; the skill materializes into the repo as a convenience
copy written by the agent. Watch for drift complaints in practice.

## Q3 — Which Go MCP SDK?

Official `modelcontextprotocol/go-sdk` vs `mark3labs/mcp-go` (older, wider adoption).
Streamable HTTP support and client compatibility (Cline's client is the historical problem
child) decide this.
**Leaning:** official SDK; verify against all three agents in Phase 1 before committing.

## Q4 — Job execution: in-process goroutines or separate worker processes?

Goroutines are simple; a wedged provider call can be abandoned via context cancellation, so
process isolation buys little.
**Leaning:** in-process with `context.WithTimeout` per stage + weighted semaphores for the
unit/slot pools. Revisit only if provider calls prove un-cancellable.

## Q5 — Coder-fusion default: on or off?

v1 default is ON (user's call). The v1 data *suggests* the value sits in planning, but no
experiment ever isolated the coder stage — the evidence is inconclusive, not negative.
**Leaning:** don't decide by fiat; decide with the isolation ablation (the ablation (ADR-009)):
same brief, coder-fusion arm vs single-coder arm, 2–3 pre-registered task pairs. If cf shows
no delta → default OFF (big latency/cost win). If it shows a win → keep ON and *then*
explore variants. Until the ablation runs, v1's default stands.

## Q6 — How does the agent authenticate to a shared HTTP server?

Localhost-only in v2.0 (bind 127.0.0.1) vs bearer token from day 1 (needed the moment it
runs on a NAS/another box).
**Leaning:** bind localhost by default; optional static bearer token env var. No OAuth until
someone actually needs it.

## Q7 — Lessons injection: how much, and who curates?

Risk: lessons file grows into noise and eats context (v1's 32K ceiling still applies to
Featherless models).
**Leaning:** hard cap ~30 lines per stack, human-editable, distiller proposes appends via
artifact (like registry proposals). `lf_lessons` for visibility.

## Q8 — Does the 32K Featherless output cap need a structural answer?

v1 worked around it per-case (decision tables, move synthesis to DeepSeek). Bigger briefs
will hit it again.
**Leaning:** make "context budget per stage" a first-class engine concept (token-count
inputs, refuse/compact before calling). At minimum, log input token counts per call in
metrics to see the margin.

## Q9 — Scheduler inside the server or external cron?

Inside = self-contained container, one config. External = simpler engine.
**Leaning:** inside (a 50-line cron loop), because the container is the product boundary and
"no host setup" is a v2 goal.

## Q10 — What replaces `project_dir` gitops exactly?

Skill creates the branch pre-`lf_plan`. But v1's `ensure_clean` guard (refuse to plan on a
dirty tree) was engine-side safety.
**Leaning:** move the check into the skill contract AND have `lf_plan` require a
`git_state: {branch, clean: true}` attestation arg — the engine refuses without it. Keeps the
guarantee without host access.
