package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Status is the job lifecycle per ADR-003:
// queued → running → done | failed | cancelled | budget_exhausted.
type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusDone            Status = "done"
	StatusFailed          Status = "failed"
	StatusCancelled       Status = "cancelled"
	StatusBudgetExhausted Status = "budget_exhausted"
)

// Terminal reports whether no further transitions can happen.
func (s Status) Terminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusCancelled, StatusBudgetExhausted:
		return true
	}
	return false
}

// Key identifies a job. IDs are derived-stable from the key (ADR-003
// amendment): resubmitting the same work while it runs finds the same job.
type Key struct {
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
	Stage     string `json:"stage"`
	TaskID    string `json:"task_id"`
}

// ID returns the derived-stable job id for the key.
func (k Key) ID() string {
	h := sha256.Sum256([]byte(k.ProjectID + "\x00" + k.Slug + "\x00" + k.Stage + "\x00" + k.TaskID))
	return "job_" + hex.EncodeToString(h[:8])
}

// ErrorKind is ADR-007's taxonomy: recoverable (model dropout → degrade,
// v1 semantics) vs fatal (missing key, budget, no-progress) — always surfaced
// in job status, never a hang.
type ErrorKind string

const (
	ErrorRecoverable ErrorKind = "recoverable"
	ErrorFatal       ErrorKind = "fatal"
)

// JobError is the error surfaced in job status.
type JobError struct {
	Kind    ErrorKind `json:"kind"`
	Message string    `json:"message"`
}

// Budgets are the engine-enforced per-job limits (ADR-007). Zero values mean
// "no limit" for that dimension; config supplies stage defaults.
type Budgets struct {
	MaxWallClock   time.Duration `json:"max_wall_clock"`
	MaxModelCalls  int           `json:"max_model_calls"`
	MaxTokensTotal int           `json:"max_tokens_total"`
}

// Job is the pollable record behind lf_job (ADR-003). Snapshots of it are
// persisted on every transition so results outlive both the request and the
// agent (crash → reconnect → poll).
type Job struct {
	ID          string          `json:"id"`
	Key         Key             `json:"key"`
	Attempt     int             `json:"attempt"`
	Fingerprint string          `json:"fingerprint"`
	Status      Status          `json:"status"`
	Progress    string          `json:"progress,omitempty"`
	Partial     json.RawMessage `json:"partial,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *JobError       `json:"error,omitempty"`
	Budgets     Budgets         `json:"budgets"`
	ModelCalls  int             `json:"model_calls"`
	TokensTotal int             `json:"tokens_total"`
	SubmittedAt time.Time       `json:"submitted_at"`
	StartedAt   time.Time       `json:"started_at,omitzero"`
	FinishedAt  time.Time       `json:"finished_at,omitzero"`
}

// Persister receives a snapshot after every job transition. The store (ADR-005)
// implements it; tests use fakes. Persist must not block for long — it is
// called on the runner's transition path.
type Persister interface {
	Persist(job Job)
}

// NopPersister discards snapshots (tests, or store not yet wired).
type NopPersister struct{}

func (NopPersister) Persist(Job) {}
