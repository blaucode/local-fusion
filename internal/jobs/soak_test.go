//go:build soak

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestSoak is the M2 exit-gate soak (PROJECT-PLAN): sustained concurrent
// fake-provider jobs under -race — normal completions, cancellation storms,
// and budget expiries interleaved — with zero goroutine leaks at the end.
// Run via `make soak`.
func TestSoak(t *testing.T) {
	before := runtime.NumGoroutine()

	r := NewRunner(8, NopPersister{}, nil)

	const rounds = 25
	const perRound = 20 // 20 concurrent jobs = the exit-gate figure, times 25 rounds

	var wg sync.WaitGroup
	for round := 0; round < rounds; round++ {
		ids := make([]string, 0, perRound)
		for i := 0; i < perRound; i++ {
			mode := i % 4
			key := Key{ProjectID: "soak", Slug: fmt.Sprintf("r%d", round), Stage: "plan", TaskID: fmt.Sprintf("t%d", i)}
			var budgets Budgets
			if mode == 2 {
				budgets = Budgets{MaxWallClock: time.Duration(1+rand.Intn(10)) * time.Millisecond}
			}
			job, existing, err := r.Submit(key, "fp", budgets, func(ctx context.Context, jc *JobContext) (json.RawMessage, error) {
				jc.Progress("working")
				switch mode {
				case 0: // fast success
					return json.RawMessage(`{}`), nil
				case 1: // recoverable-error failure
					return nil, errors.New("model dropout")
				default: // wedged: dies by budget (2) or by cancel storm (3)
					select {
					case <-time.After(10 * time.Second):
						return nil, errors.New("unreachable")
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			})
			if err != nil || existing {
				t.Fatalf("submit r%d t%d: err=%v existing=%v", round, i, err, existing)
			}
			ids = append(ids, job.ID)
		}

		// Cancellation storm for the mode-3 jobs (and racy double-cancels).
		for i, id := range ids {
			if i%4 == 3 {
				wg.Add(2)
				go func(id string) { defer wg.Done(); r.Cancel(id) }(id)
				go func(id string) { defer wg.Done(); r.Cancel(id) }(id)
			}
		}

		// Concurrent pollers, like agents hammering lf_job.
		wg.Add(1)
		go func(ids []string) {
			defer wg.Done()
			for k := 0; k < 10; k++ {
				for _, id := range ids {
					r.Get(id)
				}
				r.List()
			}
		}(ids)

		// Wait for the round to fully terminate.
		deadline := time.Now().Add(15 * time.Second)
		for _, id := range ids {
			for {
				job, ok := r.Get(id)
				if ok && job.Status.Terminal() {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("round %d: job %s stuck non-terminal", round, id)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
	wg.Wait()
	r.Close()

	// Goroutine-leak check: allow the count to settle.
	settleDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(settleDeadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}
