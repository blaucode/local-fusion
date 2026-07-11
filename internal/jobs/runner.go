package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// StageFunc is the unit of work a job runs — an engine stage (plan,
// coder_fusion, …). It must honor ctx (all provider calls take it) and report
// through jc. Returning ctx.Err() after a budget kill is expected and mapped
// to budget_exhausted, not failure.
type StageFunc func(ctx context.Context, jc *JobContext) (json.RawMessage, error)

// Runner is the async job engine (ADR-003): bounded workers, idempotent
// submit on derived-stable IDs, cooperative cancel, engine-enforced budgets
// (ADR-007), persistence hook on every transition (ADR-005).
type Runner struct {
	mu      sync.Mutex
	active  map[string]*running // by job ID; only non-terminal jobs
	recent  map[string]Job      // last terminal snapshot by job ID
	sem     chan struct{}       // bounds concurrently running jobs
	pers    Persister
	baseCtx context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	log     *slog.Logger
}

type running struct {
	jc     *JobContext
	cancel context.CancelFunc
	// cancelled records an explicit lf_cancel, to distinguish it from a
	// budget deadline when both surface as context cancellation.
	cancelled bool
}

// NewRunner creates a runner executing at most workers jobs concurrently.
func NewRunner(workers int, pers Persister, log *slog.Logger) *Runner {
	if workers <= 0 {
		workers = 4
	}
	if pers == nil {
		pers = NopPersister{}
	}
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		active:  make(map[string]*running),
		recent:  make(map[string]Job),
		sem:     make(chan struct{}, workers),
		pers:    pers,
		baseCtx: ctx,
		stop:    cancel,
		log:     log,
	}
}

// Fingerprint hashes the submit arguments; same key + different fingerprint
// while active is a conflict (ADR-003 amendment).
func Fingerprint(args any) string {
	b, _ := json.Marshal(args)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// Submit enqueues fn under key. Idempotent: if the same (key, fingerprint) is
// queued or running, the existing job is returned with existing=true and no
// second run starts. Same key with a different fingerprint while active
// returns ErrConflict. After a terminal state, submit starts a new attempt.
func (r *Runner) Submit(key Key, fingerprint string, budgets Budgets, fn StageFunc) (Job, bool, error) {
	id := key.ID()

	r.mu.Lock()
	defer r.mu.Unlock()

	if act, ok := r.active[id]; ok {
		act.jc.mu.Lock()
		snapshot := *act.jc.job
		act.jc.mu.Unlock()
		if snapshot.Fingerprint != fingerprint {
			return Job{}, false, ErrConflict
		}
		return snapshot, true, nil
	}

	attempt := 1
	if prev, ok := r.recent[id]; ok {
		attempt = prev.Attempt + 1
	}

	job := &Job{
		ID:          id,
		Key:         key,
		Attempt:     attempt,
		Fingerprint: fingerprint,
		Status:      StatusQueued,
		Budgets:     budgets,
		SubmittedAt: time.Now().UTC(),
	}

	jobCtx, cancel := context.WithCancel(r.baseCtx)
	jc := &JobContext{job: job, runner: r, cancel: cancel}
	r.active[id] = &running{jc: jc, cancel: cancel}
	// Snapshot before the worker goroutine exists — after `go`, job fields
	// may only be touched under jc.mu.
	snapshot := *job
	r.persist(snapshot)

	r.wg.Add(1)
	go r.run(jobCtx, cancel, jc, fn)

	return snapshot, false, nil
}

func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, jc *JobContext, fn StageFunc) {
	defer r.wg.Done()
	defer cancel()

	// Respect the worker bound; cancellation while queued is honored.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		r.finish(jc, nil, ctx.Err())
		return
	}

	jc.mu.Lock()
	jc.job.Status = StatusRunning
	jc.job.StartedAt = time.Now().UTC()
	budgets := jc.job.Budgets
	snapshot := *jc.job
	jc.mu.Unlock()
	r.persist(snapshot)

	runCtx := ctx
	var budgetCancel context.CancelFunc
	if budgets.MaxWallClock > 0 {
		runCtx, budgetCancel = context.WithTimeout(ctx, budgets.MaxWallClock)
		defer budgetCancel()
	}

	result, err := fn(runCtx, jc)
	if err == nil && runCtx.Err() != nil {
		// A stage must not report success after its context died (kill-switch
		// integrity: a budget kill can't be laundered into done).
		err = runCtx.Err()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		// The wall-clock budget fired; classify regardless of how the stage
		// surfaced its death. Stage-internal timeouts don't reach here — only
		// runCtx's own deadline does.
		err = fmt.Errorf("%w (%s)", ErrBudgetWallClock, budgets.MaxWallClock)
	}
	r.finish(jc, result, err)
}

// finish maps the stage outcome onto the lifecycle and persists the terminal
// snapshot. Error taxonomy per ADR-007.
func (r *Runner) finish(jc *JobContext, result json.RawMessage, err error) {
	r.mu.Lock()
	act := r.active[jc.job.ID]
	explicitCancel := act != nil && act.cancelled
	r.mu.Unlock()

	jc.mu.Lock()
	job := jc.job
	job.FinishedAt = time.Now().UTC()
	switch {
	case err == nil:
		job.Status = StatusDone
		job.Result = result
	case explicitCancel:
		job.Status = StatusCancelled
		job.Error = &JobError{Kind: ErrorFatal, Message: "cancelled via lf_cancel"}
	case isBudgetErr(err):
		job.Status = StatusBudgetExhausted
		job.Error = &JobError{Kind: ErrorFatal, Message: err.Error()}
	case errors.Is(err, ErrNoProgress):
		job.Status = StatusFailed
		job.Error = &JobError{Kind: ErrorFatal, Message: err.Error()}
	case errors.Is(err, context.Canceled):
		// Runner shutdown (not lf_cancel): still cancelled, never a hang.
		job.Status = StatusCancelled
		job.Error = &JobError{Kind: ErrorFatal, Message: "server shutting down"}
	default:
		job.Status = StatusFailed
		job.Error = &JobError{Kind: ErrorFatal, Message: err.Error()}
	}
	snapshot := *job
	jc.mu.Unlock()

	r.mu.Lock()
	delete(r.active, snapshot.ID)
	r.recent[snapshot.ID] = snapshot
	r.mu.Unlock()

	r.persist(snapshot)
	r.log.Info("job finished",
		"job", snapshot.ID, "stage", snapshot.Key.Stage, "slug", snapshot.Key.Slug,
		"status", snapshot.Status, "attempt", snapshot.Attempt,
		"model_calls", snapshot.ModelCalls, "tokens", snapshot.TokensTotal)
}

func isBudgetErr(err error) bool {
	return errors.Is(err, ErrBudgetWallClock) || errors.Is(err, ErrBudgetModelCalls) || errors.Is(err, ErrBudgetTokens)
}

// Get returns the current snapshot for lf_job polling.
func (r *Runner) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if act, ok := r.active[id]; ok {
		act.jc.mu.Lock()
		snapshot := *act.jc.job
		act.jc.mu.Unlock()
		return snapshot, true
	}
	job, ok := r.recent[id]
	return job, ok
}

// List returns snapshots of all known jobs (active first), for lf_status
// rediscovery (ADR-003 amendment).
func (r *Runner) List() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.active)+len(r.recent))
	for _, act := range r.active {
		act.jc.mu.Lock()
		out = append(out, *act.jc.job)
		act.jc.mu.Unlock()
	}
	for _, job := range r.recent {
		out = append(out, job)
	}
	return out
}

// Cancel cooperatively cancels a job (lf_cancel). Partial artifacts are
// preserved — they were persisted as they were written.
func (r *Runner) Cancel(id string) bool {
	r.mu.Lock()
	act, ok := r.active[id]
	if ok {
		act.cancelled = true
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	act.cancel()
	return true
}

// Close cancels everything and waits for workers to drain (graceful shutdown).
func (r *Runner) Close() {
	r.stop()
	r.wg.Wait()
}

func (r *Runner) persist(job Job) {
	r.pers.Persist(job)
}
