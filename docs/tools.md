# Tool reference

All tools return structured JSON with an `ok` field; failures are structured
(`{ok: false, error: "..."}`) rather than protocol errors, so agents can branch on them.

The async pattern (long stages): submit returns a `job_id` in under 2 seconds; poll
`lf_job` every 30–60 seconds until the status is terminal. Job state survives server
restarts and agent crashes — rediscover job ids with `lf_status`.

## lf_coder_fusion

Submit async implementation of a planned task (returns a `job_id`; poll with
[`lf_job`](#lf_job)). Requires the task's `plan.md` and `acceptance.md` — run
[`lf_plan`](#lf_plan) first.

Default is the fusion path: two coders implement in parallel, an evaluator picks the
stronger as BASE and names grafts from the other, and a lead merges. Every rung degrades
gracefully — one coder fails → the survivor ships; the evaluator fails → base A, no
grafts; the lead fails → the chosen base ships ungrafted. `solo: true` uses a single
coder.

**Args:** `project_id`, `slug`, `task_id`, `task_slug`, optional `context`, optional
`pipeline`, optional `solo`, optional `budget` (defaults: 15 min wall clock, 8 calls).

**Result** (via `lf_job`): `{files: [{path, content}], base_chosen, notes}` — proposed
files are returned **as data**; your agent applies them to the repo and then runs tests
(the server never touches your filesystem). They are also persisted under
`build/<task>/proposed/` in the artifact volume.

## lf_plan

Submit async planning for a slug. Returns a `job_id` in under 2 seconds — poll with
[`lf_job`](#lf_job) every 30–60s. Long-running by design (minutes per task); the job
survives agent crashes and disconnects.

By default each task gets the full deliberation: haft (frame → explore → compare), a
TL panel that hunts for gaps, and a synthesizer that merges everything into the final
three-part brief (ADR/PLAN/ACCEPTANCE). Pass `no_fusion: true` for the faster solo path
(haft only). If the synthesizer fails mid-run, the task degrades gracefully to the
deliberation output instead of aborting the plan.

**Before calling:** create `feature/<slug>` from a clean tree (the server never touches
your repo — you attest instead), and have your human-owned intent ready.

**Args:** `project_id`, `slug`, `request`, optional `context` (code the agent gathered),
optional `pipeline`, optional `no_fusion`, optional `force`, optional `budget`
(`{max_wall_clock_seconds, max_model_calls, max_tokens_total}`), plus two **required
attestations**:

- `git_state`: `{branch, base_branch, clean: true}` — refused otherwise.
- `intent`: `{tier, ref, approved_by, drafted_by}` — the loop refuses goal-free runs.
  Tiers: `feature` (ref = PRD/ADR path or URL), `fix` (ref = approved brief or issue),
  `chore` (ref = a **charter id**; see below). `drafted_by` may be `agent` — authorship
  is free, ownership is not. Both attestations are recorded in `request.md` and the
  manifest for audit.

**Returns:** `{ok, job_id, existing, status}` — `existing: true` means an identical job
was already queued/running (idempotent resubmit). Same slug with *different* arguments
while running returns a conflict; `lf_cancel` first.

**Result** (via `lf_job` when done): the manifest — tasks with per-task briefs written
to the store (`scope.md`, `tasks/<id>-<slug>/{adr,plan,acceptance,context}.md`).

### Charters (chore-tier intent)

A charter is a standing, human-approved authorization for a class of chores. Create one
by dropping `charters/<id>.json` into the data volume:

```json
{"id": "weekly-deps", "title": "Weekly dependency bumps",
 "approved_by": "your-name", "created_at": "2026-07-13T00:00:00Z",
 "expires": "2026-10-01T00:00:00Z"}
```

`expires` is optional. A chore-tier `lf_plan` referencing a missing, unapproved, or
expired charter is refused.

## The gate loop (what your agent does per task)

1. Implement the task; run the test suite yourself.
2. `lf_review` with the changed files → fix the findings that matter.
3. Re-run tests; call `lf_judge` with the changed files **and the test report**.
4. PASS ⇔ tests green AND average score ≥ 8.0. A red test run makes PASS impossible —
   no model can override the test runner.

Until planning runs on the server (M3), supply the task brief once via the `brief`
argument (either tool); it is stored as the task's `plan.md` and reused afterwards.

## lf_review

Multi-model code review of an implementation against its task brief. Synchronous.

**Args:** `project_id`, `slug`, `task_id`, `task_slug`, `changed_files` (full file
contents, concatenated), optional `pipeline`, optional `brief` (first call only).

**Returns:** `{ok, critical, important, minor, findings: [{model_key, text}],
review_md}` — `review.md` is also persisted in the artifact volume.

## lf_judge

The quality gate: dual-judge scoring plus the deterministic test gate. Synchronous
(reasoning judges can take minutes).

**Args:** as `lf_review`, plus **`test_report`**: `{command, exit_code, summary}` from
the test run you just executed. Malformed reports are rejected outright — a gate that
silently ignores bad evidence is worse than no gate.

**Returns:** `{ok, verdict, avg, req, sec, maint, gate_reason?, judges[], verdict_md,
attempt}`. `verdict` is `PASS` only when `exit_code == 0` **and** the score average is
≥ 8.0. Every run appends a `metrics.jsonl` record (schema `build-2.0`).

**Judge-retry ledger (ADR-007):** the manifest tracks judge attempts per task. After two
rounds on the same task, a third call returns `verdict: "escalate_to_human"` with
`escalated: true` and **runs no judges** — v1's "re-judge once, then stop" convention is
now enforced. Stop the fix→re-judge loop and get a person.

## lf_job

Poll an async job.

**Args:** `job_id` (from an async submit).

**Returns:** `{ok, job: {job_id, stage, task_id, attempt, status, progress, partial,
result, error, model_calls, tokens_total, submitted_at}}`

- `status`: `queued | running | done | failed | cancelled | budget_exhausted` — the
  last four are terminal.
- `progress` is stage-granular (e.g. `"task 2/4: TL panel 1/3"`), for narration.
- `partial` holds whatever the job produced before a cancel/budget kill.
- `error.kind` is `recoverable` or `fatal`; budget kills name the exhausted budget.

## lf_cancel

Cooperatively cancel a running job. Artifacts and partial results written so far are
preserved.

**Args:** `job_id`.

**Returns:** `{ok, cancelled, status?}` — cancelling an already-finished job is a no-op
(`cancelled: false` plus the terminal status).

## lf_status

Manifest plus all known jobs for a work slug — including jobs submitted before a server
restart. This is the rediscovery tool: an agent that crashed mid-poll calls this to find
its `job_id`s again.

**Args:** `project_id` (opaque; use the repo name), `slug`.

**Returns:** `{ok, manifest?, jobs: [JobView...], providers?}` — `manifest` is absent
until planning has run for the slug (not an error). `providers` is a per-provider
observability snapshot since server start: `[{base_url, calls, errors, avg_latency_ms}]`.
