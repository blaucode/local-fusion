package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
)

// harness spins up the full seam: real store, real runner, real Streamable
// HTTP transport, real SDK client — exactly what an agent sees.
type harness struct {
	store   *store.Store
	runner  *jobs.Runner
	session *sdk.ClientSession
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := jobs.NewRunner(2, st, nil)
	t.Cleanup(runner.Close)

	srv := NewServer()
	RegisterTools(srv, Deps{Runner: runner, Store: st})
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "tools-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: http.DefaultClient,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &harness{store: st, runner: runner, session: session}
}

func (h *harness) call(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		t.Fatalf("%s returned tool error: %+v", tool, res.Content)
	}
	out, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLfJobPollsThroughTerminal(t *testing.T) {
	h := newHarness(t)

	key := jobs.Key{ProjectID: "repo", Slug: "sl", Stage: "plan", TaskID: "t1"}
	job, _, err := h.runner.Submit(key, "fp", jobs.Budgets{}, func(ctx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
		jc.Progress("task 1/1: planning")
		return json.RawMessage(`{"tasks":1}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var out map[string]any
	for {
		out = h.call(t, "lf_job", map[string]any{"job_id": job.ID})
		if out["ok"] != true {
			t.Fatalf("lf_job not ok: %v", out)
		}
		status := out["job"].(map[string]any)["status"].(string)
		if jobs.Status(status).Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job never terminal via lf_job")
		}
		time.Sleep(10 * time.Millisecond)
	}
	jv := out["job"].(map[string]any)
	if jv["status"] != "done" || jv["result"].(map[string]any)["tasks"] != float64(1) {
		t.Fatalf("lf_job terminal view = %v", jv)
	}

	// Unknown job: structured error pointing at lf_status, not a protocol error.
	unknown := h.call(t, "lf_job", map[string]any{"job_id": "job_0000000000000000"})
	if unknown["ok"] != false || unknown["error"] == nil {
		t.Fatalf("unknown job = %v", unknown)
	}
}

func TestLfCancelRunningJob(t *testing.T) {
	h := newHarness(t)

	started := make(chan struct{})
	key := jobs.Key{ProjectID: "repo", Slug: "sl", Stage: "plan", TaskID: "t1"}
	job, _, _ := h.runner.Submit(key, "fp", jobs.Budgets{}, func(ctx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started

	out := h.call(t, "lf_cancel", map[string]any{"job_id": job.ID})
	if out["ok"] != true || out["cancelled"] != true {
		t.Fatalf("lf_cancel = %v", out)
	}

	// Second cancel: terminal no-op with status reported.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out = h.call(t, "lf_cancel", map[string]any{"job_id": job.ID})
		if out["cancelled"] == false && out["status"] == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancel no-op never observed: %v", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLfStatusManifestJobsAndRestartRediscovery(t *testing.T) {
	h := newHarness(t)

	// No manifest yet: ok, no error, empty jobs (planning hasn't run).
	out := h.call(t, "lf_status", map[string]any{"project_id": "repo", "slug": "sl"})
	if out["ok"] != true || out["manifest"] != nil {
		t.Fatalf("empty status = %v", out)
	}

	// Manifest + a finished job appear.
	if _, err := h.store.InitSlug("repo", "sl", "req", "main", "feature/sl", false); err != nil {
		t.Fatal(err)
	}
	key := jobs.Key{ProjectID: "repo", Slug: "sl", Stage: "plan", TaskID: "t1"}
	job, _, _ := h.runner.Submit(key, "fp", jobs.Budgets{}, func(ctx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
		return nil, nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if j, ok := h.runner.Get(job.ID); ok && j.Status.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job stuck")
		}
		time.Sleep(5 * time.Millisecond)
	}

	out = h.call(t, "lf_status", map[string]any{"project_id": "repo", "slug": "sl"})
	if out["manifest"].(map[string]any)["branch"] != "feature/sl" {
		t.Fatalf("manifest missing: %v", out)
	}
	if len(out["jobs"].([]any)) != 1 {
		t.Fatalf("jobs = %v", out["jobs"])
	}

	// Restart rediscovery: a fresh runner (empty memory) still lists the job
	// from the store (ADR-003 amendment).
	fresh := jobs.NewRunner(2, h.store, nil)
	defer fresh.Close()
	srv := NewServer()
	RegisterTools(srv, Deps{Runner: fresh, Store: h.store})
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	defer ts.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "restart", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "lf_status", Arguments: map[string]any{"project_id": "repo", "slug": "sl"}})
	if err != nil || res.IsError {
		t.Fatalf("post-restart lf_status: %v %v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var restart struct {
		Jobs []JobView `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &restart); err != nil {
		t.Fatal(err)
	}
	if len(restart.Jobs) != 1 || restart.Jobs[0].JobID != job.ID {
		t.Fatalf("restart rediscovery failed: %+v", restart.Jobs)
	}
}
