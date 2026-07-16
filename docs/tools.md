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
4. PASS ⇔ tests green AND average score ≥ 8.0 AND every acceptance criterion covered. A red
   test run or an uncovered criterion makes PASS impossible — no model can override them.

If you're reviewing or judging a change that wasn't planned through `lf_plan`, supply the
task brief once via the `brief` argument (either tool); it's stored as the task's `plan.md`
and reused on later calls for that task.

## lf_review

Multi-model code review of an implementation against its task brief. **Async** (a
reviewer panel runs sequentially and can exceed client timeouts): returns a `job_id`; poll
[`lf_job`](#lf_job) until `done`, then read the findings from `job.result`.

**Args:** `project_id`, `slug`, `task_id`, `task_slug`, `changed_files` (full file
contents, concatenated), optional `pipeline`, optional `brief` (first call only).

**Returns:** `{ok, critical, important, minor, findings: [{model_key, text}],
review_md}` — `review.md` is also persisted in the artifact volume.

## lf_judge

The quality gate: dual-judge scoring plus two deterministic gates — the test gate and the
acceptance-coverage gate. **Async** (a dual reasoning-judge round is minutes and exceeds
client timeouts): returns a `job_id`; poll [`lf_job`](#lf_job) until `done` and read the
verdict from `job.result`. **Exception:** the judge-retry escalation (below) is returned
*synchronously* — a third attempt answers instantly with `escalate_to_human` and no job.

**Args:** as `lf_review`, plus **`test_report`**: `{command, exit_code, summary}` from
the test run you just executed (malformed reports are rejected outright), and optionally
**`acceptance_coverage`**: one evidence string per acceptance criterion, in the order they
appear in the task's `acceptance.md` — the test or code that proves it.

**Submit returns:** `{ok, job_id, existing, status}` — or, when the retry ledger is
exhausted, `{ok, verdict: "escalate_to_human", escalated: true, attempt, gate_reason}` with
no job. **`job.result`** (via `lf_job`): `{verdict, avg, req, sec, maint, gate_reason?,
judges[], verdict_md, attempt, acceptance_criteria?, acceptance_uncovered?}`. `verdict` is
`PASS` only when `exit_code == 0` **and** the average is ≥ 8.0 **and** every acceptance
criterion is covered. Every run appends a `metrics.jsonl` record (schema `build-2.0`).

**Acceptance-coverage gate (ADR-014):** when the task's `acceptance.md` has criteria, PASS
requires every one attested-covered. Call `lf_judge` once with no `acceptance_coverage` to
get the parsed `acceptance_criteria` back, then pass one evidence string per criterion (in
order). Missing or blank entries force FAIL and are named in `acceptance_uncovered` and
`verdict.md`. A task with no acceptance criteria is unaffected. This makes "we built every
thing the brief asked for" a deterministic guarantee, not a judge opinion.

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

## lf_reload

Hot-reload `providers.yaml` (models, pipelines, enable/disable a provider) without
restarting the server — v1 required a restart after any config change.

**Args:** none.

**Returns:** `{ok, path, models, pipelines, providers}` on success. A config that fails to
parse is **rejected** and the previously loaded one is kept (`{ok: false, error, note}`) —
a typo never takes the gate down. In-flight jobs keep the config snapshot they started
with; new submissions use the reloaded config.

> API keys arrive via the container's environment and are read per call; changing a key
> still needs a container restart (a runtime constraint, not a config-file one).
