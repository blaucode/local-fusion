# local-fusion v2.0 — Architecture

> Go engine, containerized MCP server (Streamable HTTP), async job model, engine-owned
> artifact store. The v1 division of labor survives intact and gets *stricter*: the engine
> never touches the host at all.
> Created: 2026-07-07

---

## 1. Topology

```
┌─────────────────────────────┐          ┌──────────────────────────────────────┐
│ CODING AGENT (host)         │   HTTP   │ local-fusion v2 (container)          │
│ Claude Code / Cline / Cursor│◄────────►│                                      │
│ • explores repo, selects    │  MCP     │ ┌──────────┐  ┌───────────────────┐  │
│   context                   │(Streamable│ │ MCP      │  │ Job runner        │  │
│ • applies proposed files    │  HTTP)   │ │ surface  │─►│ • queue, workers  │  │
│ • runs tests → test report  │          │ │ lf_* tools│  │ • budgets/caps    │  │
│ • ALL git operations        │          │ └──────────┘  │ • no-progress det.│  │
│ • fixes findings            │          │       │       └─────────┬─────────┘  │
└─────────────────────────────┘          │       ▼                 ▼            │
         ▲                               │ ┌──────────┐  ┌───────────────────┐  │
         │ files/artifacts as data       │ │ Engine   │  │ Scheduler         │  │
         └───────────────────────────────│ │ plan/cf/ │  │ discover/eval cron│  │
                                         │ │ review/  │  └───────────────────┘  │
                                         │ │ judge    │  ┌───────────────────┐  │
                                         │ └────┬─────┘  │ Artifact store    │  │
                                         │      │        │ (volume) + lessons│  │
                                         │      ▼        └───────────────────┘  │
                                         │  Providers: Featherless / Ollama     │
                                         └──────────────────────────────────────┘
```

Key change vs v1: **stdio → Streamable HTTP**, one shared server instead of one process per
agent. Agents configure `"url": "http://localhost:8484/mcp"` instead of a `command`.

## 2. The isolation upgrade (load-bearing decision #1, revised)

v1 invariant: *never write the project source tree* (but it did create git branches and wrote
the `local-fusion/<slug>/` folder inside the project). A containerized server can't reliably
see the host repo anyway — so v2 makes the boundary total:

- **The engine never touches the host filesystem or repo.** No gitops in the engine.
  `create_branch` moves to the skill: the *agent* creates `feature/<slug>` before `lf_plan`.
- **Artifacts live in an engine-owned volume**, keyed `(project_id, slug)`. Every artifact is
  also **returned as data** over the tool boundary (v1 already did this for proposed files —
  v2 does it for everything).
- The skill optionally **materializes** artifacts into `<project>/local-fusion/<slug>/` so
  the in-repo paper trail survives — written by the agent, like source files. Same pattern,
  one rule, no exceptions.
- Context flows one way: the agent sends file contents *in*; nothing in the container can
  read the repo. This removes the path-traversal/exfiltration surface entirely.

Consequence: `project_dir` (v1 tool arg) becomes `project_id` (opaque string; agents use the
repo name).

## 3. Async job model (load-bearing decision #2)

Long stages (`plan`, `coder_fusion`) become jobs. Short ones (`review`, `judge`, `status`)
stay synchronous — they're 1–2 model calls.

### Tool surface

| Tool | Sync? | Notes |
|---|---|---|
| `lf_plan` | async → `job_id` | Args as v1 minus `project_dir` gitops; plus `budget` overrides |
| `lf_coder_fusion` | async → `job_id` | `solo=true` may run sync (single coder, ~1 call) |
| `lf_job` | sync | `job_id` → `{status, progress, partial, result?, error?}` |
| `lf_cancel` | sync | Cooperative cancel; artifacts written so far are preserved |
| `lf_review` | sync | Unchanged semantics |
| `lf_judge` | sync | **New required arg: `test_report`** (see §5) |
| `lf_status` | sync | Manifest + running jobs for the slug |
| `lf_lessons` | sync | Read/preview the lessons that will be injected (transparency) |
| `lf_reload` | sync | Hot-reload providers.yaml/env — kills v1's "restart after any config change" |

Job lifecycle: `queued → running → done | failed | cancelled | budget_exhausted`. Progress is
stage-granular ("task 2/4: TL panel 1/3") so the skill can narrate. Jobs and results persist
in the artifact store — an agent that crashes can reconnect and poll.

### Skill change

The skill's "this takes minutes, don't retry" warnings become a simple poll loop:
submit → poll `lf_job` every 30–60s → proceed. MCP timeouts become irrelevant.

## 4. Budgets, termination, no-progress (engine-enforced)

Per job, with config defaults and per-call overrides:

- `max_wall_clock` (e.g. plan: 30m, coder-fusion: 15m)
- `max_model_calls` (step cap)
- `max_tokens_total` (even flat-rate plans have opportunity cost — this bounds latency)
- **No-progress detection**: a stage that returns the same failure twice consecutively (e.g.
  synthesis overflow twice with same input) fails the job instead of degrading silently.
- **Judge retry ledger**: v1's "fix, re-judge once" was a skill convention; v2 records
  judge attempts per task in the manifest and `lf_judge` refuses a third attempt with
  `escalate_to_human`. The convention becomes a guarantee.
- Error taxonomy: `recoverable` (model dropout → degrade, as v1's `call_model → None`) vs
  `fatal` (missing key, budget, no-progress) — surfaced in job status, never a hang.

## 5. Deterministic verification in the gate

v1 gap: the agent runs tests but the judge never sees results — an LLM-only PASS is possible
on red tests. v2:

```
lf_judge(project_id, slug, task_id, changed_files, test_report, task_label)
  test_report: {command, exit_code, summary, failures[]}   ← produced by the agent
  gate: PASS ⇔ exit_code == 0 AND avg ≥ 8.0
```

The dual-judge panel still scores req/sec/maint — but it *cannot* override the test runner.
Judges additionally receive the report (calibrates "requirements" scoring, v1's coverage-gap
blind spot). The skill already makes the agent run tests before review; this just makes the
evidence part of the contract.

## 6. Memory loop (Reflexion)

New engine component, deliberately small:

1. Every verdict/review finding appends structured records (v1's metrics.jsonl, kept).
2. A distiller (scheduled or on-demand) compresses recurring findings into
   `lessons/<stack>.md` — max ~30 lines, human-editable, versioned in the artifact volume.
3. `plan` injects the matching lessons file into the haft *frame* step and the synthesizer
   prompt. `lf_lessons` lets you see/prune what's being injected.

Acceptance: the v1-documented repeat blind spot (non-integer JSON input untested, flagged on
both T14 and T15) must be the first lesson, and must show up in the next plan's acceptance
checklist without human prompting.

## 7. Scheduler (automations)

Cron-style jobs inside the server (config-defined, no host cron needed):

- `discover` weekly per provider → diff vs registry → artifact "new models" report.
- `eval` new candidates against the reference implementation (v1's T25 haft-fusion baseline)
  → proposal artifact with role scores. Human approves → registry update → `lf_reload`.
- Lessons distillation nightly.

Registry writes remain **proposal + approve**, never silent — same philosophy as code.

## 8. Go engine layout

```
cmd/local-fusion/          main: serve | discover | eval | run (CLI parity)
internal/mcp/              Streamable HTTP MCP surface (official Go MCP SDK)
internal/jobs/             queue, workers, budgets, persistence
internal/engine/
    plan/  coderfusion/  review/  judge/     (ports of orchestrator/fusion/*)
    providers/             one HTTP client (replaces curl subprocess), retries, streaming
    registry/              providers.yaml load/validate/hot-reload (schema unchanged)
internal/store/            artifact volume, manifest, metrics, lessons
internal/sched/            cron jobs
prompts/                   ALL prompts as embedded template files — not Go string literals
```

Notes:
- **Prompts and providers.yaml are data, not code.** The v1 prompt wording and config schema
  are the hard-won assets; the port must not paraphrase them. `//go:embed` + text/template.
- Panels use goroutines + `errgroup` with the same provider pool semantics (Featherless
  units vs Ollama slots) modeled as weighted semaphores — v1's concurrency doc becomes types.
- v1's "curl because Cloudflare blocks urllib" concern: Go's `net/http` with a real TLS stack
  and proper User-Agent is expected to pass; verify against Featherless in week 1 (see
  OPEN-QUESTIONS.md — fallback is a curl exec shim, ugly but proven).

## 9. Container & config

- Image: distroless/static, one binary, `prompts/` embedded. Volume: `/data` (artifacts,
  lessons, metrics, job state). Secrets: env vars (`FEATHERLESS_API_KEY`, `OLLAMA_API_KEY`)
  or mounted env file — never baked into the image.
- `docker run -p 8484:8484 -v lf-data:/data --env-file providers.env local-fusion:2`
- Health endpoint for the skill to check before submitting (`GET /healthz`).
- Backward-compat: `local-fusion serve --stdio` kept during migration so v1 configs work.
