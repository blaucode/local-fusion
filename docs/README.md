# local-fusion — user documentation

Docs for **users** of local-fusion: developers who run the quality gate and drive it from
a coding agent. Design and implementation docs live in
[product-docs/](../product-docs/PROJECT-PLAN.md).

Start here:

1. [Quickstart](./quickstart.md) — run the server, verify it's healthy, connect your agent.
2. [MCP setup per agent](./mcp-setup.md) — Claude Code, Cline, Cursor, OpenCode.
3. [The skill](./skill.md) — install the agent-side operating guide that drives the loop.
4. [Usage](./usage.md) — a full walkthrough of one feature: plan → build → review → judge.
5. [Configuration](./configuration.md) — keys, provider registry, constitution, auth, flags.
6. [Tool reference](./tools.md) — every `lf_*` tool, its arguments, and what it returns.

## What local-fusion is

A containerized MCP server that exposes a multi-model quality gate as a set of `lf_*`
tools. Your coding agent owns the repo, git, and the working tree; local-fusion does the
multi-model thinking — planning deliberation, two competing coders merged into one, a
reviewer panel, and a dual-judge gate backed by a deterministic test-and-coverage check.
The server never touches your filesystem; everything crosses the boundary as data.

The full tool surface is served over both transports (Streamable HTTP and stdio):
`lf_plan`, `lf_coder_fusion`, `lf_review`, `lf_judge`, the job tools (`lf_job`,
`lf_cancel`, `lf_status`), and `lf_reload`. The long stages run asynchronously — submit
returns a `job_id`, and you poll `lf_job` until it's done.
