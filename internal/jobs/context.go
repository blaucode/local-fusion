package jobs

import (
	"errors"
	"fmt"
	"sync"
)

// Budget/termination sentinels (ADR-007). All are fatal-kind when they fail a job.
var (
	ErrBudgetWallClock  = errors.New("budget_exhausted: max_wall_clock")
	ErrBudgetModelCalls = errors.New("budget_exhausted: max_model_calls")
	ErrBudgetTokens     = errors.New("budget_exhausted: max_tokens_total")
	ErrNoProgress       = errors.New("no_progress: same failure twice consecutively")
	ErrConflict         = errors.New("conflict: job with same key but different arguments is active — lf_cancel it first")
)

// JobContext is handed to the stage function. It is the stage's only channel
// for progress narration, partial results, and budget accounting — the runner
// owns the law (ADR-007), the stage merely reports.
type JobContext struct {
	mu       sync.Mutex
	job      *Job
	runner   *Runner
	lastFail string
	cancel   func()
}

// Progress updates the stage-granular progress string (e.g. "task 2/4: TL panel 1/3").
func (jc *JobContext) Progress(s string) {
	jc.mu.Lock()
	jc.job.Progress = s
	snapshot := *jc.job
	jc.mu.Unlock()
	jc.runner.persist(snapshot)
}

// SetPartial records partial output; preserved on cancel/budget kill (ADR-003).
func (jc *JobContext) SetPartial(data []byte) {
	jc.mu.Lock()
	jc.job.Partial = append([]byte(nil), data...)
	snapshot := *jc.job
	jc.mu.Unlock()
	jc.runner.persist(snapshot)
}

// StartModelCall accounts one model call against the step cap. The stage must
// call it before each provider call and stop on error (the job context is also
// cancelled, so in-flight HTTP dies with it).
func (jc *JobContext) StartModelCall() error {
	jc.mu.Lock()
	jc.job.ModelCalls++
	over := jc.job.Budgets.MaxModelCalls > 0 && jc.job.ModelCalls > jc.job.Budgets.MaxModelCalls
	jc.mu.Unlock()
	if over {
		jc.cancel()
		return ErrBudgetModelCalls
	}
	return nil
}

// AddTokens accounts token usage from a completed call against the token budget.
func (jc *JobContext) AddTokens(n int) error {
	jc.mu.Lock()
	jc.job.TokensTotal += n
	over := jc.job.Budgets.MaxTokensTotal > 0 && jc.job.TokensTotal > jc.job.Budgets.MaxTokensTotal
	jc.mu.Unlock()
	if over {
		jc.cancel()
		return ErrBudgetTokens
	}
	return nil
}

// RecordFailure implements no-progress detection (ADR-007): a stage reporting
// the same failure signature twice consecutively must abort instead of
// degrading silently. The stage calls it on every recoverable failure and
// stops on error.
func (jc *JobContext) RecordFailure(signature string) error {
	jc.mu.Lock()
	same := jc.lastFail != "" && jc.lastFail == signature
	jc.lastFail = signature
	jc.mu.Unlock()
	if same {
		jc.cancel()
		return fmt.Errorf("%w: %s", ErrNoProgress, signature)
	}
	return nil
}

// ClearFailure resets the no-progress tracker after forward progress.
func (jc *JobContext) ClearFailure() {
	jc.mu.Lock()
	jc.lastFail = ""
	jc.mu.Unlock()
}
