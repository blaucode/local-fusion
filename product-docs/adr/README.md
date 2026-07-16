# Architecture Decision Records — local-fusion v2

Process: any decision that reverses an ADR, adds a dependency, or changes a tool contract
gets a new/amended ADR **before code** (see [PROJECT-PLAN.md](../PROJECT-PLAN.md)).
Statuses: Proposed → Accepted → Superseded. M1 spike results amend 001/002 with evidence.

| ADR | Decision | Status |
|---|---|---|
| [001](./ADR-001-go-rewrite.md) | Rewrite engine in Go, single static binary | Accepted — M1 evidence in (S1/Q1 PASS) |
| [002](./ADR-002-transport-and-container.md) | Streamable HTTP primary + stdio kept; Docker; bearer token | Accepted — M1 evidence in (S2/Q3 PASS, 3/3 clients) |
| [003](./ADR-003-async-job-model.md) | Async submit→poll job model for long stages | Accepted — amended 2026-07-16 (review + judge now async) |
| [004](./ADR-004-filesystem-free-server.md) | Filesystem-free server; agent owns files/git; attestation | Accepted |
| [005](./ADR-005-artifact-store.md) | Engine-owned artifact store; agent materializes in-repo copy | Accepted |
| [006](./ADR-006-deterministic-test-gate.md) | Deterministic test gate outranks LLM judges | Accepted — shipped in v1 |
| [007](./ADR-007-budgets-and-termination.md) | Engine-enforced budgets, retry ledger, no-progress exits | Accepted |
| [008](./ADR-008-model-agnostic-providers-and-prompts.md) | Two provider clients; prompts/config frozen as data | Accepted |
| [009](./ADR-009-coder-fusion-pending-ablation.md) | Coder-fusion: port verbatim, decide by ablation | Accepted (default undecided by design; amended: judge prerequisite) |
| [010](./ADR-010-deterministic-parity.md) | Port parity via record/replay, judges off the critical path | Accepted (supersedes ±0.5 judge-score parity) |
| [011](./ADR-011-spec-anchored-intent-contract.md) | Spec-anchored intent contract: `lf_plan` refuses without human-owned intent (tiered: PRD / brief / charter) | Accepted |
| [012](./ADR-012-project-constitution.md) | Project constitution: persistent principles injected (parity-safe) into plan + judge | Accepted |
| [013](./ADR-013-clarification-gate.md) | Pre-plan clarification gate (skill-side now; `lf_clarify` tool design-first) | Accepted |
| [014](./ADR-014-acceptance-coverage-gate.md) | Acceptance-coverage gate: every acceptance criterion must be attested covered | Accepted |

**2026-07-09 external review** produced: amendments to 002 (capacity policy, stdio-retreat
honesty), 003 (reconnect/idempotency mechanics), 009 (ablation instrument prerequisite);
new 010; new [DATA-GOVERNANCE.md](../DATA-GOVERNANCE.md). Each amendment carries its
rationale inline — the ADRs are the complete record.
