# local-fusion v2 — user docs

> Docs for **users** of local-fusion (developers running the quality gate).
> Implementation docs live in [product-docs/](../product-docs/PROJECT-PLAN.md).

- [Quickstart](./quickstart.md) — run the server, check it's healthy, connect your agent
- [Configuration](./configuration.md) — keys, auth token, flags
- [MCP setup per agent](./mcp-setup.md) — Claude Code, Cline, Cursor
- [Tool reference](./tools.md) — the `lf_*` tools

**Status (M2, in progress):** the server runs with both transports (Streamable HTTP +
stdio), health checking, token auth, the async job engine, and the job tools
(`lf_job`, `lf_cancel`, `lf_status`). The submit tools (`lf_review`, `lf_judge`,
`lf_plan`, `lf_coder_fusion`) land next — each ships with its docs section.
