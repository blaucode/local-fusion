# The local-fusion skill

The skill is the agent-side operating guide for the loop: it tells your coding agent how
to drive the eight `lf_*` tools — gather context, own git, submit and poll the async
stages, apply proposed files, run tests, and stop when the gate says
`escalate_to_human`.

The canonical, agent-agnostic source lives in this repo at
[`skill/local-fusion/SKILL.md`](../skill/local-fusion/SKILL.md), versioned with the binary.
Install it into your agent's skills directory (copy or symlink):

| Agent | Location |
|---|---|
| Claude Code | `~/.claude/skills/local-fusion/SKILL.md` (or a project `.claude/skills/…`) |
| Cline | `.cline/skills/local-fusion/SKILL.md` in the workspace |
| Cursor | `.cursor/skills/local-fusion/SKILL.md` in the workspace |

```sh
# example: install for Claude Code, symlinked so it tracks the repo
mkdir -p ~/.claude/skills/local-fusion
ln -sf "$(pwd)/skill/local-fusion/SKILL.md" ~/.claude/skills/local-fusion/SKILL.md
```

The content is identical across agents — the loop is agent-agnostic. Keep the symlink (or
re-copy) so the skill tracks the server version; the skill and the tool surface evolve
together.

## Using it

Once installed and the [server is running](./quickstart.md) and
[connected](./mcp-setup.md), invoke the skill (e.g. `/local-fusion`) or just ask the agent
to "build this with local-fusion". The agent will: gather repo context, settle
human-owned intent with you, create the feature branch, plan, then per task run
coder-fusion → test → review → judge, showing you the plan and the verdicts along the way.

See the [tool reference](./tools.md) for what each tool the skill calls returns.
