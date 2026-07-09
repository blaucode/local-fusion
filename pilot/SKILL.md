# local-fusion-gate — Quality gate for agent-built features (Team Pilot)

Use this skill when asked to "build X with the quality gate", "gate this feature", or when
this repo's definition-of-done requires a local-fusion verdict.

## What this is

You (the agent) build the feature as you normally would. local-fusion makes you **prove the
work**: a planned brief, a test report, and two independent judge models scoring the result.
PASS requires green tests AND an average judge score ≥ 8.0. The verdict and paper trail land
in `local-fusion/<slug>/` for the PR.

You need the `local-fusion` MCP server connected (see PILOT.md in this folder).

## The loop

1. **Gather context.** Read the repo. Select the files this feature touches plus similar
   existing code (entities, controllers, services, routes, tests) the implementation should
   imitate. Concatenate as `=== path ===\n<content>` blocks. Quality of this selection is
   your highest-leverage step.

2. **Plan (cheap mode).** Call `lf_plan(project_dir, slug, request, context, no_fusion=true)`.
   This creates the `feature/<slug>` branch and writes a brief (`plan.md`), ADR, and
   acceptance checklist per task. It takes a few minutes — that is normal; do NOT retry or
   call other `lf_*` tools while waiting. (`no_fusion=true` = single-model planning. Drop it
   only if the task is genuinely ambiguous/architecturally hard and you want the full panel —
   it costs more time.)

3. **Per task, in dependency order:**

   a. **Implement it yourself.** Read `local-fusion/<slug>/tasks/NN-<task>/plan.md` and
      `acceptance.md`. Build exactly what the brief says, following the repo's conventions.

   b. **Test.** Run the project's test suite. Fix failures. Capture a test report:
      `{"command": "<what you ran>", "exit_code": <int>, "summary": "<one line>"}`.
      Do not proceed on red tests.

   c. **Judge.** Call `lf_judge(project_dir, slug, task_id, changed_files, task_label,
      test_report)` where `changed_files` is your changed files concatenated as
      `=== path ===\n<content>` and `test_report` is the JSON string from step b.
      ALWAYS pass the test report — a non-zero exit_code forces FAIL regardless of scores.
      Never judge untested code.

   d. On **PASS**: next task. On **FAIL**: fix the specific issues in the judge's notes,
      re-run tests, judge **once** more. If it fails again, STOP and report to the human —
      do not loop.

4. **Report.** Summarize per task: verdict, scores (req/sec/maint/avg), tests status, and
   point to `local-fusion/<slug>/build/<task>/verdict.md`. Leave the branch for human review —
   do not merge.

## Invariants (do not violate)

- YOU write all source files and run all tests. local-fusion never touches the source tree.
- The test report must be real — the actual command, the actual exit code. Never fabricate it.
- One judge retry maximum, then escalate to the human.
- Don't re-run `lf_plan` on a judge FAIL — the plan is fine; fix the implementation.
- `lf_*` calls take minutes. Waiting is normal. Never call a second `lf_*` tool while one runs.
