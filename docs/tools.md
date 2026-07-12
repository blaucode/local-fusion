# Tool reference

All tools return structured JSON with an `ok` field; failures are structured
(`{ok: false, error: "..."}`) rather than protocol errors, so agents can branch on them.

The async pattern (long stages): submit returns a `job_id` in under 2 seconds; poll
`lf_job` every 30–60 seconds until the status is terminal. Job state survives server
restarts and agent crashes — rediscover job ids with `lf_status`.

> M2 note: the submit tools (`lf_plan`, `lf_coder_fusion`, `lf_review`, `lf_judge`)
> land later in M2/M3; this page documents what the current build serves.

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
