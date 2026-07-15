package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
)

func newPlanHarness(t *testing.T, caller providers.Caller) (*sdk.ClientSession, *store.Store, *jobs.Runner) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
providers:
  p1: {base_url: "https://p1/v1", env_key: K1}
models:
  tl-1: {provider: p1, id: v/tl1, roles: [tl], scores: {tl: 9.0}}
pipelines:
  default:
    tl_panel: {n: 1}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.NewHolder(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := jobs.NewRunner(2, st, nil)
	t.Cleanup(runner.Close)

	srv := NewServer()
	engine := EngineDeps{Store: st, Cfg: cfg, Caller: caller, Log: func(string) {}}
	RegisterTools(srv, Deps{Runner: runner, Store: st})
	RegisterPlanTool(srv, PlanDeps{Engine: engine, Runner: runner})
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "plan-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, st, runner
}

func planArgs() map[string]any {
	return map[string]any{
		"project_id": "repo", "slug": "feat", "request": "Build the thing",
		"context": "ctx", "no_fusion": true, // test pipeline has no synthesizer
		"git_state": map[string]any{
			"branch": "feature/feat", "base_branch": "main", "clean": true,
		},
		"intent": map[string]any{
			"tier": "feature", "ref": "product-docs/PRD.md",
			"approved_by": "adolfo", "drafted_by": "human",
		},
	}
}

func TestLfPlanSubmitPollDone(t *testing.T) {
	caller := &scriptedCaller{responses: []string{
		`[{"slug":"api","title":"API","summary":"s","deps":[]}]`,
		"F", "E", "## ADR\na\n## PLAN\np\n## ACCEPTANCE\nc",
	}}
	session, st, _ := newPlanHarness(t, caller)

	out := callStage(t, session, "lf_plan", planArgs())
	if out["ok"] != true || out["existing"] == true {
		t.Fatalf("submit = %v", out)
	}
	jobID := out["job_id"].(string)

	// Poll lf_job until terminal — the ADR-003 loop, over the wire.
	deadline := time.Now().Add(10 * time.Second)
	var jv map[string]any
	for {
		res := callStage(t, session, "lf_job", map[string]any{"job_id": jobID})
		jv = res["job"].(map[string]any)
		if jobs.Status(jv["status"].(string)).Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never terminal: %v", jv)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if jv["status"] != "done" {
		t.Fatalf("job = %v", jv)
	}
	manifest := jv["result"].(map[string]any)["manifest"].(map[string]any)
	if manifest["branch"] != "feature/feat" || manifest["intent"].(map[string]any)["approved_by"] != "adolfo" {
		t.Fatalf("manifest = %v", manifest)
	}

	// Store has the full slug tree.
	if _, err := st.ReadArtifact("repo", "feat", "tasks/01-api/plan.md"); err != nil {
		t.Fatal(err)
	}

	// Idempotent resubmit after terminal: new attempt (fingerprint identical).
	out = callStage(t, session, "lf_plan", planArgs())
	if out["ok"] != true {
		t.Fatalf("resubmit = %v", out)
	}
}

func TestLfPlanRefusals(t *testing.T) {
	session, _, _ := newPlanHarness(t, &scriptedCaller{})

	// Missing intent → refusal naming the three tiers.
	args := planArgs()
	delete(args, "intent")
	args["intent"] = map[string]any{}
	out := callStage(t, session, "lf_plan", args)
	if out["ok"] != false || !strings.Contains(out["error"].(string), "\"feature\"") {
		t.Fatalf("intent refusal = %v", out)
	}

	// Dirty tree → git_state refusal.
	args = planArgs()
	args["git_state"] = map[string]any{"branch": "feature/feat", "base_branch": "main", "clean": false}
	out = callStage(t, session, "lf_plan", args)
	if out["ok"] != false || !strings.Contains(out["error"].(string), "git_state") {
		t.Fatalf("git refusal = %v", out)
	}

	// Chore without a charter → refusal pointing at charters.
	args = planArgs()
	args["intent"] = map[string]any{"tier": "chore", "ref": "weekly-deps", "approved_by": "a", "drafted_by": "human"}
	out = callStage(t, session, "lf_plan", args)
	if out["ok"] != false || !strings.Contains(out["error"].(string), "charter") {
		t.Fatalf("charter refusal = %v", out)
	}
}

func TestLfPlanChoreWithValidCharter(t *testing.T) {
	caller := &scriptedCaller{responses: []string{
		`[{"slug":"deps","title":"Deps","summary":"s","deps":[]}]`,
		"F", "E", "## PLAN\np",
	}}
	session, st, _ := newPlanHarness(t, caller)

	charter, _ := json.Marshal(map[string]any{
		"id": "weekly-deps", "title": "Weekly dependency bumps",
		"approved_by": "adolfo", "created_at": time.Now().UTC(),
	})
	if err := os.MkdirAll(filepath.Join(st.Root(), "charters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Root(), "charters", "weekly-deps.json"), charter, 0o644); err != nil {
		t.Fatal(err)
	}

	args := planArgs()
	args["slug"] = "chore-run"
	args["intent"] = map[string]any{"tier": "chore", "ref": "weekly-deps", "approved_by": "adolfo", "drafted_by": "agent"}
	out := callStage(t, session, "lf_plan", args)
	if out["ok"] != true {
		t.Fatalf("chore with charter = %v", out)
	}
}

func TestLfPlanConflictWhileRunning(t *testing.T) {
	// A caller that blocks so the first job stays running.
	release := make(chan struct{})
	caller := &blockingCaller{release: release}
	session, _, _ := newPlanHarness(t, caller)

	out := callStage(t, session, "lf_plan", planArgs())
	if out["ok"] != true {
		t.Fatalf("first submit = %v", out)
	}

	// Same key, same args while running → existing job, no double-run.
	out = callStage(t, session, "lf_plan", planArgs())
	if out["ok"] != true || out["existing"] != true {
		t.Fatalf("idempotent = %v", out)
	}

	// Same key, DIFFERENT args while running → conflict.
	args := planArgs()
	args["request"] = "Something else entirely"
	out = callStage(t, session, "lf_plan", args)
	if out["ok"] != false || !strings.Contains(out["error"].(string), "conflict") {
		t.Fatalf("conflict = %v", out)
	}
	close(release)
}

type blockingCaller struct{ release chan struct{} }

func (b *blockingCaller) CallModel(ctx context.Context, _ providers.CallRequest) (string, bool) {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return "", false
}
