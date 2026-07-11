// Package jobs is the job runner: queue, workers, engine-enforced budgets
// (wall-clock, step, token), no-progress detection, judge-retry ledger, and
// persistence for reconnect/idempotent submit. Governing decisions: ADR-003,
// ADR-007. The riskiest net-new code: all of it runs under -race and the soak
// test is an M2 exit gate. Ships in M2.
package jobs
