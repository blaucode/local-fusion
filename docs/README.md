# local-fusion v2 — user docs

> Docs for **users** of local-fusion (developers running the quality gate).
> Implementation docs live in [product-docs/](../product-docs/PROJECT-PLAN.md).

- [Quickstart](./quickstart.md) — run the server, check it's healthy, connect your agent
- [Configuration](./configuration.md) — keys, auth token, flags
- [MCP setup per agent](./mcp-setup.md) — Claude Code, Cline, Cursor

**Status (M2, in progress):** the server runs with both transports (Streamable HTTP +
stdio), health checking, and token auth. The `lf_*` tools land incrementally during M2 —
each ships with its section in these docs.
