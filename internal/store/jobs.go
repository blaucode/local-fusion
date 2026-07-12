package store

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"local-fusion/internal/jobs"
)

// Persist implements jobs.Persister: every job transition snapshot lands in
// jobs/<job_id>.json (ADR-003: results outlive requests; a crashed agent —
// or a restarted server — rediscovers via lf_status). Errors are logged, not
// returned: persistence must never wedge the runner's transition path.
func (s *Store) Persist(job jobs.Job) {
	// Compact marshal: MarshalIndent would re-indent the Result/Partial
	// RawMessage bytes, changing them on every persist→load round trip.
	data, err := json.Marshal(job)
	if err != nil {
		slog.Error("store: marshal job snapshot", "job", job.ID, "err", err)
		return
	}
	// Job IDs are engine-derived ("job_" + hex), not agent input, but validate
	// anyway — this file writes into the volume root.
	if err := validateID("job_id", job.ID); err != nil {
		slog.Error("store: refusing job snapshot", "err", err)
		return
	}
	if err := atomicWrite(filepath.Join(s.root, "jobs", job.ID+".json"), append(data, '\n')); err != nil {
		slog.Error("store: persist job snapshot", "job", job.ID, "err", err)
	}
}

// LoadJob returns one persisted snapshot by job id (lf_job fallback for jobs
// that predate a server restart).
func (s *Store) LoadJob(id string) (jobs.Job, bool) {
	if err := validateID("job_id", id); err != nil {
		return jobs.Job{}, false
	}
	data, err := os.ReadFile(filepath.Join(s.root, "jobs", id+".json"))
	if err != nil {
		return jobs.Job{}, false
	}
	var job jobs.Job
	if err := json.Unmarshal(data, &job); err != nil {
		slog.Warn("store: corrupt job snapshot", "job", id, "err", err)
		return jobs.Job{}, false
	}
	return job, true
}

// LoadJobs returns the last persisted snapshot of every known job — used at
// startup so lf_status can list pre-restart jobs (any that were mid-flight
// show their last persisted state; the runner does not resurrect them).
func (s *Store) LoadJobs() ([]jobs.Job, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "jobs"))
	if err != nil {
		return nil, err
	}
	out := make([]jobs.Job, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "jobs", e.Name()))
		if err != nil {
			slog.Warn("store: skipping unreadable job snapshot", "file", e.Name(), "err", err)
			continue
		}
		var job jobs.Job
		if err := json.Unmarshal(data, &job); err != nil {
			slog.Warn("store: skipping corrupt job snapshot", "file", e.Name(), "err", err)
			continue
		}
		out = append(out, job)
	}
	return out, nil
}
