package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func testKey(task string) Key {
	return Key{ProjectID: "proj", Slug: "slug", Stage: "plan", TaskID: task}
}

// waitTerminal polls until the job leaves the active set or the deadline hits.
func waitTerminal(t *testing.T, r *Runner, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := r.Get(id); ok && job.Status.Terminal() {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal state", id)
	return Job{}
}

func TestSubmitRunsToDone(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	job, existing, err := r.Submit(testKey("t1"), Fingerprint("args"), Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		jc.Progress("task 1/1: working")
		return json.RawMessage(`{"ok":true}`), nil
	})
	if err != nil || existing {
		t.Fatalf("submit: err=%v existing=%v", err, existing)
	}
	if job.Status != StatusQueued || job.ID != testKey("t1").ID() {
		t.Fatalf("submitted job = %+v", job)
	}

	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusDone || string(done.Result) != `{"ok":true}` || done.Attempt != 1 {
		t.Fatalf("done = %+v", done)
	}
}

func TestIdempotentSubmitAndConflict(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	release := make(chan struct{})
	fp := Fingerprint(map[string]string{"brief": "A"})
	first, _, err := r.Submit(testKey("t1"), fp, Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		<-release
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Same key + same fingerprint while running → same job, no double-run.
	second, existing, err := r.Submit(testKey("t1"), fp, Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		t.Error("second stage function must never run")
		return nil, nil
	})
	if err != nil || !existing || second.ID != first.ID {
		t.Fatalf("idempotent submit: err=%v existing=%v id=%s want %s", err, existing, second.ID, first.ID)
	}

	// Same key + different fingerprint while running → conflict (ADR-003 amendment).
	_, _, err = r.Submit(testKey("t1"), Fingerprint(map[string]string{"brief": "B"}), Budgets{}, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}

	close(release)
	waitTerminal(t, r, first.ID)

	// After terminal: resubmit allowed, recorded as a new attempt.
	third, existing, err := r.Submit(testKey("t1"), Fingerprint("new args"), Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		return nil, nil
	})
	if err != nil || existing {
		t.Fatalf("resubmit after terminal: err=%v existing=%v", err, existing)
	}
	if third.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", third.Attempt)
	}
	waitTerminal(t, r, third.ID)
}

func TestCancelIsCooperativeAndPreservesPartial(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	started := make(chan struct{})
	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		jc.SetPartial([]byte(`{"tasks_done":1}`))
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started

	if !r.Cancel(job.ID) {
		t.Fatal("cancel returned false for a running job")
	}
	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusCancelled {
		t.Fatalf("status = %s, want cancelled", done.Status)
	}
	if string(done.Partial) != `{"tasks_done":1}` {
		t.Fatalf("partial lost on cancel: %q", done.Partial)
	}
	if r.Cancel("job_nonexistent") {
		t.Fatal("cancel of unknown job must return false")
	}
}

func TestWallClockBudgetKillsWedgedStage(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	start := time.Now()
	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{MaxWallClock: 100 * time.Millisecond}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		select {
		case <-time.After(10 * time.Minute): // the wedged provider call
			return nil, errors.New("unreachable")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusBudgetExhausted {
		t.Fatalf("status = %s (%+v), want budget_exhausted", done.Status, done.Error)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("kill took %v", elapsed)
	}
	if done.Error == nil || done.Error.Kind != ErrorFatal {
		t.Fatalf("budget error must be fatal-kind: %+v", done.Error)
	}
}

func TestBudgetKillCannotLaunderIntoDone(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	// A buggy/lying stage ignores its dead context and returns success.
	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{MaxWallClock: 50 * time.Millisecond}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		<-ctx.Done()
		return json.RawMessage(`{"ok":true}`), nil // lies
	})
	done := waitTerminal(t, r, job.ID)
	if done.Status == StatusDone {
		t.Fatal("stage laundered a budget kill into done — kill-switch integrity broken")
	}
	if done.Status != StatusBudgetExhausted {
		t.Fatalf("status = %s, want budget_exhausted", done.Status)
	}
}

func TestModelCallAndTokenBudgets(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	job, _, _ := r.Submit(testKey("calls"), "fp", Budgets{MaxModelCalls: 2}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		for {
			if err := jc.StartModelCall(); err != nil {
				return nil, err
			}
		}
	})
	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusBudgetExhausted || done.ModelCalls != 3 {
		t.Fatalf("step cap: status=%s calls=%d, want budget_exhausted after 3rd attempt", done.Status, done.ModelCalls)
	}

	job2, _, _ := r.Submit(testKey("tokens"), "fp", Budgets{MaxTokensTotal: 1000}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		for {
			if err := jc.AddTokens(400); err != nil {
				return nil, err
			}
		}
	})
	done2 := waitTerminal(t, r, job2.ID)
	if done2.Status != StatusBudgetExhausted || done2.TokensTotal != 1200 {
		t.Fatalf("token budget: status=%s tokens=%d", done2.Status, done2.TokensTotal)
	}
}

func TestNoProgressDetection(t *testing.T) {
	r := NewRunner(2, nil, nil)
	defer r.Close()

	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		// Different failures, then forward progress, then the same failure twice.
		if err := jc.RecordFailure("synthesis overflow"); err != nil {
			return nil, err
		}
		if err := jc.RecordFailure("model dropout"); err != nil {
			return nil, err
		}
		jc.ClearFailure()
		if err := jc.RecordFailure("synthesis overflow"); err != nil {
			return nil, err
		}
		if err := jc.RecordFailure("synthesis overflow"); err != nil {
			return nil, err // second identical → must abort here
		}
		return nil, errors.New("unreachable: no-progress not detected")
	})
	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusFailed || done.Error == nil || done.Error.Kind != ErrorFatal {
		t.Fatalf("no-progress: %+v", done)
	}
	if want := "no_progress"; len(done.Error.Message) < len(want) || done.Error.Message[:len(want)] != want {
		t.Fatalf("error message %q must name no_progress", done.Error.Message)
	}
}

func TestProgressAndPersistenceSnapshots(t *testing.T) {
	pers := &recordingPersister{}
	r := NewRunner(2, pers, nil)
	defer r.Close()

	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		jc.Progress("task 2/4: TL panel 1/3")
		return nil, nil
	})
	done := waitTerminal(t, r, job.ID)
	if done.Status != StatusDone {
		t.Fatalf("status = %s", done.Status)
	}

	statuses := pers.statuses()
	// Must have persisted at least queued → running → (progress) → done.
	if len(statuses) < 3 || statuses[0] != StatusQueued || statuses[len(statuses)-1] != StatusDone {
		t.Fatalf("persistence snapshots = %v", statuses)
	}
	if !pers.sawProgress("task 2/4: TL panel 1/3") {
		t.Fatal("progress string was never persisted")
	}
}

func TestRunnerCloseCancelsActiveJobs(t *testing.T) {
	pers := &recordingPersister{}
	r := NewRunner(2, pers, nil)

	started := make(chan struct{})
	job, _, _ := r.Submit(testKey("t1"), "fp", Budgets{}, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	r.Close()

	final, ok := r.Get(job.ID)
	if !ok || final.Status != StatusCancelled {
		t.Fatalf("after Close: %+v ok=%v, want cancelled", final, ok)
	}
}

type recordingPersister struct {
	mu   sync.Mutex
	jobs []Job
}

func (p *recordingPersister) Persist(job Job) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, job)
}

func (p *recordingPersister) statuses() []Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Status, len(p.jobs))
	for i, j := range p.jobs {
		out[i] = j.Status
	}
	return out
}

func (p *recordingPersister) sawProgress(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, j := range p.jobs {
		if j.Progress == s {
			return true
		}
	}
	return false
}
