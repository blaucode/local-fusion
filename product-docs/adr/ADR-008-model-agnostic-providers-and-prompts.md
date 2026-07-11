# ADR-008: Model-agnostic provider layer; prompts and config as frozen data

**Status:** Accepted
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

Two coupled assets decide whether v2 keeps v1's measured quality: the **provider/registry
machinery** (role-scored fitness, unit/slot concurrency pools, degradation semantics) and the
**prompt wording** of every stage — both hard-won through T0–T25. Meanwhile the team target
(PRD Goal 4) requires running on models v1 never assumed: teammates' existing OpenAI-compatible
gateways and Anthropic keys, not just Featherless/Ollama.

## Decision

Two provider client types: **`openai-compatible`** (covers Featherless, Ollama Cloud, OpenAI,
most gateways — one implementation) and **`anthropic`** (native Messages API). The
`providers.yaml` **schema is preserved** — a v1 file loads unmodified; new deployments pick a
**cost profile** preset (`flat-rate` / `byok` / `mixed`). Judge rule kept: two judges from
**different model families** (diversity is what the dual-judge needs — open weights never were
the requirement). **All prompts extracted verbatim** from v1 into `prompts/*.tmpl`,
`//go:embed`-ed, with a CI check that engine PRs don't touch prompt files (prompt changes are
their own reviewed PR — they are the product, ADR discipline applies).

## Options Considered

### A: Port provider calls inline (prompts as Go string literals) — invites silent
paraphrasing during the port, the sneakiest quality regression; couples wording to releases.
Rejected.
### B: Full gateway dependency (route everything via LiteLLM/OpenRouter) — adds a runtime
dependency and cost layer to dodge ~200 lines of client code; breaks the flat-rate premise.
Rejected.
### C: Two thin clients + frozen prompt data *(chosen)*.

## Trade-off Analysis

`openai-compatible` alone covers ~90% of targets with one client; Anthropic-native is the
one genuinely different API worth first-class support. Freezing prompts as data converts the
biggest un-testable port risk (wording drift) into a mechanically checkable one (byte diff).

## Consequences

- Easier: pilots on team keys (M4/R6); model swaps via config; parity testing (same prompts,
  both engines); future providers = config, mostly.
- Harder: two client implementations to maintain; template escaping discipline; per-model
  quirks (32K-total models needing `max_tokens=16384` on coder — carry v1's fix as config,
  not code).
- Revisit: provider-specific streaming; utls/curl-shim fallback if S1 fails (ADR-001).

## Action Items
1. [ ] M0: extraction script Python→`prompts/*.tmpl` + byte-diff CI check
2. [ ] `openai-compatible` client (S1 spike grows into it); `anthropic` client by M4
3. [ ] Cost-profile presets shipped as example `providers.yaml` variants
