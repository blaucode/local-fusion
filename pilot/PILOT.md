# Team Pilot — Quality Gate Setup (15 minutes)

> What you get: every agent-built feature arrives on a branch with a written brief, a real
> test report, and a two-judge verdict (PASS ≥ 8.0 + green tests) — reviewable in the PR.
> This pilot runs on local-fusion **v1 as it exists today** (Python). The Go/container
> version replaces this setup later without changing the workflow.

## Prerequisites

- macOS/Linux, `python3.14`, `curl`, `bash`
- API key for at least one OpenAI-compatible inference provider. The reference setup uses
  Featherless ($25/mo flat) + Ollama Cloud, but any OpenAI-compatible endpoint can be
  configured in `config/providers.yaml`. (Anthropic-native API support comes with v2.)
- A coding agent: Claude Code, Cline, or Cursor.

## Setup

```bash
# 1. Clone the tool (not into your project)
git clone <local-fusion repo url> ~/tools/local-fusion
cd ~/tools/local-fusion
pip3.14 install --break-system-packages -r requirements.txt

# 2. Keys
cp config/providers.env.example config/providers.env
# edit: FEATHERLESS_API_KEY=... / OLLAMA_API_KEY=... (or your provider)

# 3. Sanity check
bin/local-fusion discover   # should list models, not errors
```

## Connect your agent

Add to your agent's MCP config (Claude Code: `.mcp.json`; Cline/Cursor: MCP settings).
**The timeout matters** — planning takes minutes:

```json
{
  "mcpServers": {
    "local-fusion": {
      "command": "/absolute/path/to/python3.14",
      "args": ["/Users/<you>/tools/local-fusion/orchestrator/server.py"],
      "timeout": 3600
    }
  }
}
```

Auto-approve the `lf_*` tools if your agent supports it.

## Install the skill in your project repo

```bash
mkdir -p .claude/skills/local-fusion-gate   # or .cline/ / .cursor/
cp ~/code/blautech/local-fusion-v2/pilot/SKILL.md .claude/skills/local-fusion-gate/SKILL.md
git add .claude && git commit -m "add local-fusion quality gate skill"
```

## Run your first gated feature

In your agent, on a clean working tree:

> Build with the quality gate: add a `GET /api/health` endpoint that returns build version
> and DB connectivity status, with tests.

The agent will: plan (minutes — normal) → implement → run tests → judge with the test report
attached. You get a `feature/<slug>` branch plus `local-fusion/<slug>/` containing
`scope.md`, per-task `plan.md`/`adr.md`/`acceptance.md`, and `build/<task>/verdict.md`.

## What to look at (especially architects)

- `verdict.md` — scores (req/sec/maint), tests GREEN/RED line, both judges' notes.
- `plan.md` + `acceptance.md` — did the brief capture what you'd have asked for?
- The gate is hard: red tests = FAIL no matter what the judges score.

## Rules of the pilot

1. Real repos, real tasks — not toys, but start with small well-scoped features.
2. If you stop using it, say why — a dropped pilot is data we act on.
3. The threshold (8.0) and rubric are fixed for the pilot; collect gripes, we tune after.
4. Never merge on verdict alone — the gate is evidence for review, not a replacement for it.

## Known limitations (v1, honest list)

- Stage calls are synchronous and slow (minutes); the `timeout: 3600` config is required.
- Server reads config once at startup — restart it after any config change.
- One feature at a time per repo (no parallel slugs on one working tree).
- Planning quality depends on your context selection — garbage in, garbage out.

Feedback → Adolfo. Metrics are logged automatically to the tool's `metrics.jsonl`.
