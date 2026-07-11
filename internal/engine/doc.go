// Package engine hosts the pipeline stages (plan, coder_fusion, review, judge)
// ported from v1 orchestrator/fusion/*.py — the reference implementation.
// Port rules: prompts come from prompts/*.tmpl verbatim (ADR-008); parity is
// deterministic record/replay (ADR-010); gate semantics are sacred (ADR-006:
// PASS ⇔ tests green AND avg ≥ threshold); coder-fusion is ported last and
// verbatim (ADR-009). Ships across M3.
package engine
