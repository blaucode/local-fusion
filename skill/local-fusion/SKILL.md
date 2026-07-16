---
name: local-fusion
description: Use to build a feature, API endpoint, or service through multi-model fusion — planning deliberation, two competing coders merged into one, a reviewer panel, and a dual-judge quality gate with a deterministic test gate. Invoke with /local-fusion or when the user says "build with local-fusion", "fusion build", or "plan and build this with local fusion". YOU gather repo context, own all git, apply and test code; local-fusion does the multi-model thinking over MCP.
---

# local-fusion

## What this does

Builds a feature through a multi-model pipeline: **plan deliberation → two competing
coders merged into one solution → reviewer panel → dual-judge quality gate**. The split of
responsibility is strict and load-bearing:

- **You (the agent) own:** gathering repo context, ALL git operations, applying proposed
  files to the working tree, running tests, and materializing the artifact trail.
- **local-fusion owns:** the multi-model thinking (plan, coder-fusion, review, judge),
  exposed as MCP tools. **The server never touches your filesystem or git** — it can't see
  your repo at all. Everything crosses the boundary as data.

Because the server is filesystem-free, two things are different from a local tool:
it is addressed by an opaque `project_id` (use the repo name), not a path; and the long
stages are **asynchronous** — you submit and poll.

## Prerequisites

The local-fusion MCP server must be connected over HTTP. Confirm these tools are available:
`lf_plan`, `lf_coder_fusion`, `lf_job`, `lf_cancel`, `lf_review`, `lf_judge`, `lf_status`,
`lf_reload`.

If they are not, the server isn't connected. Tell the user to start it and add it by URL —
see `docs/quickstart.md` and `docs/mcp-setup.md`. In short: `make docker-run`, then point
the agent at `http://localhost:8484/mcp` (Streamable HTTP). Do not proceed without these
tools. A quick liveness check is `GET http://localhost:8484/healthz` → `ok`.

## The async pattern (read once)

`lf_plan` and `lf_coder_fusion` are **jobs**: they return a `job_id` in under 2 seconds.
Poll `lf_job(job_id)` every 30–60 seconds until `status` is terminal
(`done` | `failed` | `cancelled` | `budget_exhausted`). The `progress` string narrates the
stage ("task 2/4: TL panel 1/3") — relay it. On `done`, the result is in `job.result`.
This survives disconnects and restarts: if you lose a `job_id`, `lf_status` lists it again.
`lf_review`, `lf_judge`, and `lf_status` are synchronous (1–2 model calls).

## Step 1 — Gather context (YOUR job)

Read the repo and select the files this feature touches: models, routes, controllers,
middleware, services, and any similar existing code the coders should imitate. Include
`AGENTS.md` / `CLAUDE.md` if present. Assemble the relevant file contents and conventions
into one context string.

This selection is precisely why an agent drives local-fusion — you are far better at it
than a human typing paths. Too little context and the coders guess; too much and they
drown. Pick the files that define the patterns to follow.

## Step 2 — Establish human-owned intent (REQUIRED)

local-fusion refuses to plan without human-owned intent — the loop never runs on a
goal-free "improve the code" prompt. Before planning, settle the `intent` attestation with
the user:

```
intent: {
  tier:        "feature" | "fix" | "chore",
  ref:         "<PRD path/URL, issue link, or charter id>",
  approved_by: "<human identifier>",
  drafted_by:  "human" | "agent"     # you may draft it; a human must own it
}
```

- **feature** → `ref` is the PRD/spec path or URL. Gather that spec into your context.
- **fix** → `ref` is the approved brief or issue link.
- **chore** → `ref` is a **charter id** — a standing authorization the server checks exists
  and hasn't expired (see `docs/tools.md#lf_plan`). If there's no charter for this class of
  chore, ask the user to create one; don't invent the id.

If the user has no spec/issue/charter, stop and get one. That is the point of the gate.

## Step 3 — Prepare git (YOUR job) and attest

The server can't touch your repo, so you create the branch and **attest** to its state:

1. Ensure the working tree is clean (commit or stash first).
2. Create `feature/<slug>` from the base branch.
3. Pass the `git_state` attestation to `lf_plan`:

```
git_state: { branch: "feature/<slug>", base_branch: "<base>", clean: true }
```

`clean: true` must be truthful — it stands in for the server's old `ensure_clean` check,
and the attestation is recorded in the artifact trail.

## Step 4 — Plan (async)

Call `lf_plan` with: `project_id` (the repo name), `slug` (short readable feature name,
e.g. `vendor-api`), `request` (the user's prompt verbatim), `context` (Step 1),
`git_state` (Step 3), `intent` (Step 2). Optional: `no_fusion: true` for the faster
solo-deliberation path (default runs the TL panel + synthesizer).

It returns `{job_id}`. **Poll `lf_job(job_id)`** until `done`. The result is the manifest:
`{tasks: [{id, slug, title, deps, status}]}`. Read the per-task briefs — either from
`job.result` or via `lf_status(project_id, slug)` — and **show the user the plan before
building**.

Each task has `plan.md`, `adr.md`, `acceptance.md`, `context.md`. Optionally materialize
the artifact trail into the repo at `local-fusion/<slug>/` (write the files yourself, like
source — the server keeps the canonical copy in its volume; the in-repo copy is a reviewable
convenience committed with the feature).

## Step 5 — Per task, in dependency order

Use `id` from the task list as `task_id` (e.g. `"01"`) and `slug` as `task_slug`. Respect
`deps`. For each task:

a. **Coder-fusion (async).** Call `lf_coder_fusion(project_id, slug, task_id, task_slug,
   context)` (add `solo: true` for a single coder). Poll `lf_job` until `done`. The result
   is `{files: [{path, content}], base_chosen, notes}`. **YOU apply these files** to the
   working tree — write each, reviewing before you write. local-fusion only proposes.

b. **Test.** Run the project's tests; fix obvious breakage yourself. Capture a report:
   `{"command": "<cmd>", "exit_code": <int>, "summary": "<one line>"}`.

c. **Review.** Call `lf_review(project_id, slug, task_id, task_slug, changed_files)` where
   `changed_files` is the applied files concatenated as `=== path ===\n<content>` per file.
   Fix the critical and important findings.

d. **Judge.** Call `lf_judge(project_id, slug, task_id, task_slug, changed_files,
   test_report, acceptance_coverage)`. **ALWAYS pass `test_report`** — a non-zero
   `exit_code` forces FAIL regardless of judge scores (the test runner outranks the
   models). Never judge untested code. **Also pass `acceptance_coverage`** whenever the
   task has acceptance criteria: one evidence string per criterion (the test/code that
   proves it), in the order they appear in `acceptance.md`. An uncovered criterion forces
   FAIL just like a red test — this guarantees you built everything the brief asked for.
   Unsure of the criteria? Call once without coverage; the response returns
   `acceptance_criteria` to attest against.
   - **PASS** → continue to the next task.
   - **FAIL** → fix per the judge's notes, then re-judge. Do NOT re-run `lf_coder_fusion`;
     fix the specific findings yourself.
   - **`verdict: "escalate_to_human"`** (`escalated: true`) → the task has already been
     judged twice; the loop refuses a third round. **Stop and get a person.** Do not keep
     looping.

If planning hasn't run for a task (e.g. you're reviewing/judging a hand-written change),
pass the task brief once via the `brief` argument to `lf_review`/`lf_judge`.

Use `lf_status(project_id, slug)` any time to read the manifest, task statuses, running
jobs, and per-provider health counters.

## Step 6 — Report

Summarize per task: what was built, files changed, judge scores. If you materialized the
artifact trail, commit `local-fusion/<slug>/` with the feature.

## Do NOT

- Do NOT expect local-fusion to touch your repo or git — it can't. YOU create the branch,
  apply files, and commit. The `git_state`/`intent` attestations are your honesty contract.
- Do NOT proceed without human-owned intent, or with a dirty tree attested as clean.
- Do NOT poll faster than ~30s or treat a long-running job as broken — jobs take minutes
  (many model calls). Relay the `progress` string; the job survives disconnects.
- Do NOT re-run `lf_coder_fusion` after a judge FAIL — fix the findings, then re-judge.
- Do NOT keep judging after `escalate_to_human` — that verdict means stop and get a human.
- Do NOT run the full pipeline for a trivial one-file change — say so and just do it.
- If a call genuinely errors it returns `{ok: false, error}` — read it, don't guess.
