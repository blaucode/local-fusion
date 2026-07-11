# CLAUDE.md — local-fusion v2

> **Read [AGENTS.md](./AGENTS.md) first — it is canonical** (read order, rules, current
> milestone discovery). This file adds only Claude-specific session discipline. Where they
> disagree, AGENTS.md wins.

## Session start

1. Follow the AGENTS.md read order (PROJECT-PLAN → PRD → ARCHITECTURE → relevant ADRs).
2. If the agentmemory MCP is connected: search memory for the current milestone (e.g.
   "M1 spike", "parity", "provider quirk") before starting — prior sessions may have
   already answered your question.
3. If codegraph is available: ensure this repo and `../../vendo/local-fusion` are indexed.

## Memory rules (MANDATORY, same discipline as v1)

Save to agentmemory (`memory_save`) **immediately after every result — never defer or batch**:

- Spike run completes → save outcome, exact error/success evidence, decision implied
- Exit-gate item passes/fails → save evidence link and what remains
- Parity run → save stage, result, any canonicalization issues found
- Any gotcha (SDK bug, provider quirk, Cloudflare behavior, Cline oddity) → save immediately
- Any mid-session decision or ADR amendment → save the what and the why

If a file-based memory directory is configured for this project, save findings to BOTH
systems — never one without the other. A finding lost at session end cannot be recovered.

## Claude-specific notes

- Long provider calls and spikes: prefer running them via scripts committed to the repo
  (reproducible) over ad-hoc shell one-liners.
- Use the `haft` skill for OPEN-QUESTIONS deliberation and ADR drafting when available.
- When porting from `orchestrator/fusion/*.py`: read the actual Python in the same session
  you write the Go — never port from a doc summary or from memory of a previous session.
- Never print or echo `providers.env` contents; keys must not appear in logs, argv, or chat.
