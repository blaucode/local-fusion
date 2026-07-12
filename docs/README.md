# local-fusion v2 — user docs

> Docs for **users** of local-fusion (developers running the quality gate).
> Implementation docs live in [product-docs/](../product-docs/PROJECT-PLAN.md).

- [Quickstart](./quickstart.md) — run the server, check it's healthy, connect your agent
- [Configuration](./configuration.md) — keys, auth token, flags
- [MCP setup per agent](./mcp-setup.md) — Claude Code, Cline, Cursor
- [Tool reference](./tools.md) — the `lf_*` tools

**Status (M2, in progress):** the server runs with both transports (Streamable HTTP +
stdio), health checking, token auth, the async job engine, the job tools (`lf_job`,
`lf_cancel`, `lf_status`), and the quality gate itself — `lf_review` and `lf_judge`
(dual-judge + deterministic test gate). The async planning tools (`lf_plan`,
`lf_coder_fusion`) land in M3.
