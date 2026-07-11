// Package store is the engine-owned artifact store on the /data volume, keyed
// (project_id, slug): artifact graph, manifest, job state, metrics.jsonl
// (schema build-2.0: adds user/repo/server_version/tests_green, never changes
// existing fields), charters (ADR-011), lessons. Governing decisions: ADR-004
// (filesystem-free server), ADR-005 (volume canonical, agent materializes).
// Ships in M2.
package store
