# Architecture Decision Records — local-fusion v2

Process: any decision that reverses an ADR, adds a dependency, or changes a tool contract
gets a new/amended ADR **before code** (see [PROJECT-PLAN.md](../PROJECT-PLAN.md)).
Statuses: Proposed → Accepted → Superseded. M1 spike results amend 001/002 with evidence.

| ADR | Decision | Status |
|---|---|---|
| [001](./ADR-001-go-rewrite.md) | Rewrite engine in Go, single static binary | Accepted (pending M1 evidence) |
| [002](./ADR-002-transport-and-container.md) | Streamable HTTP primary + stdio kept; Docker; bearer token | Accepted (pending M1 evidence) |
| [003](./ADR-003-async-job-model.md) | Async submit→poll job model for long stages | Accepted |
| [004](./ADR-004-filesystem-free-server.md) | Filesystem-free server; agent owns files/git; attestation | Accepted |
| [005](./ADR-005-artifact-store.md) | Engine-owned artifact store; agent materializes in-repo copy | Accepted |
| [006](./ADR-006-deterministic-test-gate.md) | Deterministic test gate outranks LLM judges | Accepted — shipped in v1 |
| [007](./ADR-007-budgets-and-termination.md) | Engine-enforced budgets, retry ledger, no-progress exits | Accepted |
| [008](./ADR-008-model-agnostic-providers-and-prompts.md) | Two provider clients; prompts/config frozen as data | Accepted |
| [009](./ADR-009-coder-fusion-pending-ablation.md) | Coder-fusion: port verbatim, decide by ablation | Accepted (default undecided by design) |
