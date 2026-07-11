# MCP setup per agent

All three agents connect the same way: **by URL** to `http://localhost:8484/mcp`
(Streamable HTTP). Start the server first ([quickstart](./quickstart.md)) and check
`curl http://localhost:8484/healthz` returns `ok`.

If you set `LF_AUTH_TOKEN` ([configuration](./configuration.md#auth)), add the
`Authorization: Bearer <token>` header where your agent supports custom headers.

## Claude Code

```sh
claude mcp add --transport http local-fusion http://localhost:8484/mcp
claude mcp list    # → local-fusion: … ✔ Connected
```

## Cline (VS Code)

Cline panel → MCP Servers → Remote Servers, or edit `cline_mcp_settings.json`:

```json
{
  "mcpServers": {
    "local-fusion": {
      "type": "streamableHttp",
      "url": "http://localhost:8484/mcp"
    }
  }
}
```

Reload VS Code; the server shows a green dot in Cline's MCP list.

## Cursor

Cursor Settings → MCP → Add server, or edit `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "local-fusion": {
      "url": "http://localhost:8484/mcp"
    }
  }
}
```

## Verifying

Ask the agent to list its MCP tools: it should report the `lf_*` tools this server
exposes. (During M2 the tool list is still growing — an empty list means you're on a
pre-tools build; `local-fusion version` tells you which.)

All three clients above were verified against this server's transport during the M1 S2
spike (see `product-docs/adr/ADR-002-transport-and-container.md`).
