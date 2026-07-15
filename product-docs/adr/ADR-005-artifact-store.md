# ADR-005: Engine-owned artifact store; agent materializes in-repo copies

**Status:** Accepted
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

Artifacts (briefs, reviews, verdicts, manifest, metrics) are the product's paper trail and
the substrate jobs persist into (ADR-003). With a filesystem-free server (ADR-004) they can
no longer live canonically in the project tree. Two candidate homes, each with real value:
engine volume (queryable, survives repo deletion, cross-project metrics) vs in-repo
(reviewable in PRs, greppable, the v1 habit architects liked).

## Decision

**Canonical home: the engine `/data` volume**, keyed `(project_id, slug)`, holding the
artifact graph, job state, and central `metrics.jsonl`. Every artifact is **also returned
as data** in tool results; the **skill materializes** the familiar `local-fusion/<slug>/`
folder into the repo as a convenience copy (agent-written, committed with the feature, review
it in the PR). Volume is source of truth on conflict.

## Options Considered

### A: In-repo canonical (v1 model) — fails ADR-004; per-repo metrics fragmentation. Rejected.
### B: Volume-only — loses the in-PR paper trail that is half the architect pitch. Rejected.
### C: Volume canonical + materialized copy *(chosen)* — both audiences served; drift possible
(accepted: copies are write-once per stage, and verdicts are immutable records, not documents
anyone edits).

## Trade-off Analysis

The PR-visible trail is a product feature (P2 architect stories), not a storage choice —
so it must survive; but jobs, budgets, and cross-project metrics need a server-side store
regardless. C is the only option serving both; its drift risk is minimal for append-only
artifacts.

## Consequences

- Easier: job persistence, adoption metrics (`user`/`repo` fields), future lessons
  distillation (P2), `lf_status` across restarts.
- Harder: backup story for `/data` (document: it's a docker volume, snapshot it);
  `project_id` collisions across teammates' repos (convention: git remote-derived slug).
- Revisit: retention policy once the volume grows (not before).

## Action Items
1. [x] `internal/store` schema: artifacts, jobs, metrics (M2, `0da6dd9`)
2. [ ] Skill materialization step + "volume is canonical" note in docs
3. [x] metrics.jsonl gained `user`, `repo`, `server_version` (schema `build-2.0`, M2f)
