# local-fusion v2 — user docs

> Docs for **users** of local-fusion (developers running the quality gate).
> Implementation docs live in [product-docs/](../product-docs/PROJECT-PLAN.md).

- [Quickstart](./quickstart.md) — run the server, check it's healthy, connect your agent
- [Configuration](./configuration.md) — keys, auth token, flags
- [MCP setup per agent](./mcp-setup.md) — Claude Code, Cline, Cursor
- [Tool reference](./tools.md) — the `lf_*` tools

**Status (M3, in progress):** both transports (Streamable HTTP + stdio), health
checking, token auth, the async job engine, the job tools (`lf_job`, `lf_cancel`,
`lf_status`), the quality gate (`lf_review`, `lf_judge` — dual-judge + deterministic
test gate), and async planning (`lf_plan`, currently plan-solo). Still to come:
plan-full (TL panel + synthesizer) and `lf_coder_fusion`.
