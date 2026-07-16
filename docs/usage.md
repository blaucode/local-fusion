# Usage

A full walkthrough of building one feature with local-fusion, from prompt to a judged,
committed change. It assumes the server is [running](./quickstart.md),
[connected](./mcp-setup.md), and [the skill is installed](./skill.md).

You never call the `lf_*` tools by hand — the skill drives them. This page shows what the
agent does on your behalf so you know what to expect and where you're in the loop.

## The division of labor

| You (with your agent)                          | local-fusion (the server)                     |
|------------------------------------------------|-----------------------------------------------|
| Gather repo context, own all git and files     | Plan the work (deliberation + synthesis)      |
| Apply proposed files, run the test suite        | Implement (two coders merged into one)        |
| Provide human-owned intent (the "why")          | Review (a multi-model reviewer panel)         |
| Decide what to commit and when to stop          | Judge (dual judge + deterministic gates)      |

The server never touches your filesystem or git. Everything crosses the boundary as data,
and the long stages run as **jobs**: submit returns a `job_id` in under two seconds, and
the agent polls `lf_job` until the status is terminal.

## Start a build

In your agent, invoke the skill and describe the work:

```
/local-fusion  build a vendor API: list and create vendors, JWT-protected,
using the repo's existing auth middleware and error envelope
```

From here the agent runs the loop. Each numbered step below maps to what it does.

### 1. Context and intent

The agent reads your repo and assembles the files this feature touches (models, routes,
middleware, similar existing handlers) into a context string, then settles **intent** with
you — local-fusion refuses to plan without a human-owned goal:

```
intent: { tier: "feature", ref: "docs/specs/vendor-api.md",
          approved_by: "you@example.com", drafted_by: "agent" }
```

- **feature** → `ref` is a PRD/spec path or URL.
- **fix** → `ref` is an approved brief or issue link.
- **chore** → `ref` is a charter id (a standing authorization; see
  [configuration](./configuration.md) and [tools](./tools.md#lf_plan)).

If the request is ambiguous in a way that would change the plan, the agent asks you a short
list of questions first. Answer them — a misread here propagates through every later stage.

### 2. Branch and plan

The agent ensures a clean tree, creates `feature/vendor-api`, and calls `lf_plan` with the
context, the `git_state` attestation, and the intent. It returns a `job_id`; the agent
polls and relays progress ("task 2/4: TL panel 1/3"). When it's done you get a manifest of
tasks, each with `plan.md`, `adr.md`, `acceptance.md`, and `context.md`.

**The agent shows you the plan before building.** Read it. This is the cheapest point to
correct course.

### 3. Build each task

For every task, in dependency order:

1. **Coder-fusion** — `lf_coder_fusion` runs two coders in parallel, picks the stronger as
   the base, grafts the best of the other, and a lead merges them. It returns proposed
   files as data. **The agent applies them with judgment** — it can reject a change that
   contradicts source it can see but the coder couldn't (e.g. a rewrite of untouched auth
   middleware). local-fusion only proposes; your agent owns the working tree.
2. **Test** — the agent runs your suite and captures a report
   (`{command, exit_code, summary}`). Never judge untested code.
3. **Review** — `lf_review` runs a reviewer panel and returns critical/important/minor
   findings. The agent fixes what matters.
4. **Judge** — `lf_judge` runs the dual judge plus two deterministic gates.

### 4. The verdict

`lf_judge` returns **PASS** only when all three hold:

- the test report's `exit_code` is `0`,
- the average judge score is **≥ 8.0**, and
- every acceptance criterion is **covered** (the agent passes one evidence string per
  criterion from `acceptance.md`).

A red test or an uncovered criterion forces **FAIL** no matter how high the models score —
the test runner and the acceptance list outrank the judges.

- **PASS** → on to the next task.
- **FAIL** → the agent fixes the specific findings and re-judges. It does *not* re-run the
  coder.
- **escalate_to_human** → after two judge rounds on the same task the gate refuses a third.
  The loop stops and hands the task to you. Don't keep looping — read the notes and decide.

### 5. Report and commit

When the tasks pass, the agent summarizes what was built, the files changed, and the judge
scores. You decide what to commit. Optionally the agent materializes the artifact trail
(`local-fusion/<slug>/`) alongside the feature, opens one GitHub issue per task, and opens
a pull request with the per-task verdicts — so the reviewable trail lands where your team
already reviews.

## What a real run looks like

A representative first live build (a JWT-protected service, from scratch):

- **Plan** — full deliberation, ~8 model calls, produced a 4-task manifest.
- **Coder-fusion** — one coder failed mid-run; the survivor's output shipped
  (graceful degradation), and the agent rejected a destructive middleware rewrite the
  coder proposed without the real source.
- **Review** — one critical and five important findings, all genuine; fixed.
- **Judge, attempt 1** — **FAIL at 7.83.** Tests were green, but the change missed an
  acceptance criterion (a `401` body / `WWW-Authenticate` header the plan required).
- **Judge, attempt 2** — **PASS at 9.0** after the fix.

The point: the gate is honest. It failed green-tested code that didn't fully meet spec, and
the deterministic gates caught the gap before it reached a human reviewer.

## Timing and cost

The model stages take minutes, not seconds — a reviewer panel and a dual reasoning-judge
round each run several models sequentially. This is why they're asynchronous: submit is
instant and the job survives disconnects and server restarts. If your agent loses a
`job_id`, `lf_status` lists every job for the slug so it can resume polling.

A full build spends real provider quota (roughly 15+ model calls across plan, coder,
review, and judge). Budget accordingly.

## When *not* to use it

For a trivial one-file change, skip the pipeline — the skill will say so and just make the
edit. local-fusion earns its cost on features with real acceptance criteria, not typo fixes.

## Where to look when something's off

- **Tools missing in the agent** → server not connected; recheck
  [MCP setup](./mcp-setup.md) and `curl http://localhost:8484/healthz`.
- **A stage errors with "provider registry"** → no `providers.yaml`; see
  [configuration](./configuration.md#provider-registry).
- **A job seems stuck** → poll no faster than ~30s; relay the `progress` string. Jobs are
  minutes-long by design. Provider health is in `lf_status` (calls, errors, latency).
- **Repeated FAILs** → read `verdict.md` and `acceptance_uncovered`; the gate names exactly
  what's missing.
