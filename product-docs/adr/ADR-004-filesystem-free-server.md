# ADR-004: Filesystem-free server — agent owns files and git

**Status:** Accepted
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

v1's best invariant: the engine never writes the source tree (coder output returned as data,
agent applies). But v1 still writes the in-project `local-fusion/<slug>/` artifact folder and
creates git branches — impossible for a containerized HTTP server that doesn't share the
agent's filesystem, and it's also the residual security surface (path traversal, host git).

## Decision

The server **never touches any host filesystem or repo**. All artifacts are returned as
data over the tool boundary AND persisted in the engine-owned `/data` volume keyed
`(project_id, slug)` (ADR-005). Git operations move to the **skill**: the agent creates
`feature/<slug>` before `lf_plan` and must pass a `git_state: {branch, clean: true}`
**attestation** — `lf_plan` refuses without it, preserving v1's `ensure_clean` guarantee
without host access. `project_dir` (path) becomes `project_id` (opaque string). The skill
optionally materializes the in-repo `local-fusion/<slug>/` paper-trail copy — written by the
agent, like source files: one rule, no exceptions.

## Options Considered

### A: Bind-mount the project into the container
**Pros:** v1 behavior preserved. **Cons:** per-project mounts kill the shared-server model;
container gains host write access (security story gone); docker config per repo. Rejected.

### B: Server does git over an API/agent callback
**Pros:** keeps `ensure_clean` server-side. **Cons:** invents an RPC channel for something
the agent does natively; complexity without safety gain. Rejected.

### C: Filesystem-free + attestation *(chosen)*
**Pros:** total isolation (the approvable security story for team adoption); container is
stateless w.r.t. host; strengthens rather than replaces the v1 invariant.
**Cons:** the clean-tree guarantee is now attested, not verified — a lying/buggy agent can
attest falsely (accepted: same trust level as the agent applying files, and the artifact
trail records the attestation).

## Trade-off Analysis

Team adoption requires a security answer an architect signs off in one sentence: *"the
server can't touch your machine."* Option C is that sentence. The lost server-side
verification is real but small — v1 already trusted the agent for the more dangerous half
(writing code).

## Consequences

- Easier: containerization, shared server, security review, multi-repo use.
- Harder: skill carries more responsibility (git + materialization + attestation);
  context flows one way (agent sends file contents in — consistent with v1's `changed_files`).
- Revisit if: attestation abuse actually observed in pilot → consider agent-side helper
  that generates signed git state.

## Action Items
1. [x] `git_state` required arg + refusal path in `lf_plan` (M3, `396c2c5`)
2. [ ] Skill: branch creation, attestation, artifact materialization steps
3. [x] `gitops.py` semantics kept out of the port (plan/coder ports, M3)
