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
| OpenCode | add the path to the `instructions` array in `opencode.json` (see below) |

**OpenCode** has no dedicated skills directory. Point its `instructions` array at the
SKILL.md so the loop is loaded as a rule (project root or global
`~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["skill/local-fusion/SKILL.md"]
}
```

Use an absolute path (or a checked-in copy) if you drive local-fusion from a repo other
than this one. OpenCode also honors the Claude Code skills directory
(`~/.claude/skills/local-fusion/SKILL.md`) when its Claude-compatibility is enabled — the
`instructions` entry is the explicit, portable option.

```sh
# example: install for Claude Code, symlinked so it tracks the repo
mkdir -p ~/.claude/skills/local-fusion
ln -sf "$(pwd)/skill/local-fusion/SKILL.md" ~/.claude/skills/local-fusion/SKILL.md
```

The content is identical across agents — the loop is agent-agnostic. Keep the symlink (or
re-copy) so the skill tracks the server version; the skill and the tool surface evolve
together.

## Using it

Once the skill is installed and the [server is running](./quickstart.md) and
[connected](./mcp-setup.md), invoke it (e.g. `/local-fusion`) or ask the agent to "build
this with local-fusion". The agent gathers repo context, settles human-owned intent with
you, creates the feature branch, plans, then runs coder-fusion → test → review → judge per
task, showing you the plan and each verdict along the way.

For a full end-to-end example, see the [usage walkthrough](./usage.md). For what each tool
returns, see the [tool reference](./tools.md).
