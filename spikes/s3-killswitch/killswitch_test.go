// S3 spike (M1 / ADR-007): budget kill-switch prototype.
// Proves context cancellation cuts through a fake 10-minute stage promptly,
// through an errgroup panel fan-out (the shape internal/jobs will use), with
// no goroutine leak. Run under -race.
package killswitch

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

// fakeStage blocks for 10 minutes unless its context is cancelled first —
// the "wedged provider call" scenario from ADR-007.
func fakeStage(ctx context.Context) error {
	select {
	case <-time.After(10 * time.Minute):
		return errors.New("stage ran to completion — kill-switch failed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runJob fans out a 3-stage panel under one job budget, like plan's TL panel.
func runJob(ctx context.Context, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < 3; i++ {
		g.Go(func() error { return fakeStage(ctx) })
	}
	return g.Wait()
}

func TestBudgetKillsWedgedPanel(t *testing.T) {
	start := time.Now()
	err := runJob(context.Background(), 150*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("kill took %v — budget did not cut through the wedged stage", elapsed)
	}
}

func TestCancellationStorm(t *testing.T) {
	before := runtime.NumGoroutine()

	// 20 concurrent jobs, all wedged, all killed by their budgets.
	g := new(errgroup.Group)
	for i := 0; i < 20; i++ {
		g.Go(func() error {
			if err := runJob(context.Background(), 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
				return errors.New("job survived its budget")
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	// All panel goroutines must drain; poll briefly for the count to settle.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}
