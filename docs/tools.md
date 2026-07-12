# Tool reference

All tools return structured JSON with an `ok` field; failures are structured
(`{ok: false, error: "..."}`) rather than protocol errors, so agents can branch on them.

The async pattern (long stages): submit returns a `job_id` in under 2 seconds; poll
`lf_job` every 30–60 seconds until the status is terminal. Job state survives server
restarts and agent crashes — rediscover job ids with `lf_status`.

> M2 note: the async submit tools (`lf_plan`, `lf_coder_fusion`) land in M3; this page
> documents what the current build serves.

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

**Returns:** `{ok, verdict, avg, req, sec, maint, gate_reason?, judges[], verdict_md}`.
`verdict` is `PASS` only when `exit_code == 0` **and** the score average is ≥ 8.0.
Every run appends a `metrics.jsonl` record (schema `build-2.0`).

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

**Returns:** `{ok, manifest?, jobs: [JobView...]}` — `manifest` is absent until planning
has run for the slug; that is not an error.
