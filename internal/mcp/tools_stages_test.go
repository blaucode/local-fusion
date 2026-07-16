package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// scriptedCaller returns canned model responses in order.
type scriptedCaller struct {
	responses []string
}

func (s *scriptedCaller) CallModel(context.Context, providers.CallRequest) (string, bool) {
	if len(s.responses) == 0 {
		return "", false
	}
	r := s.responses[0]
	s.responses = s.responses[1:]
	return r, true
}

func newStageHarness(t *testing.T, caller providers.Caller) (*sdk.ClientSession, *store.Store) {
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
  ja: {provider: p1, id: v/ja, roles: [judge, reviewer], scores: {judge: 9.8, reviewer: 9.0}}
  jb: {provider: p1, id: v/jb, roles: [judge, reviewer], scores: {judge: 9.5, reviewer: 8.0}}
pipelines:
  default:
    judges: {models: [ja, jb]}
    reviewer_panel: {n: 1}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.NewHolder(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer()
	RegisterStageTools(srv, EngineDeps{Store: st, Cfg: cfg, Caller: caller, User: "t", Ver: "test"})
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "stage-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, st
}

func callStage(t *testing.T, s *sdk.ClientSession, tool string, args map[string]any) map[string]any {
	t.Helper()
	res, err := s.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil || res.IsError {
		t.Fatalf("%s: err=%v res=%+v", tool, err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestGatedFlowOverMCP drives the full M2 loop an agent performs: brief in as
// data → lf_review → lf_judge with a red report (PASS impossible) → lf_judge
// green → PASS. All over real Streamable HTTP.
func TestGatedFlowOverMCP(t *testing.T) {
	caller := &scriptedCaller{responses: []string{
		// lf_review (reviewer_panel n=1)
		"FINDING: minor nit\nSEVERITY: minor\nFILE: a.go\nRESOLUTION: tidy",
		// lf_judge #1 (red tests) — two judges scoring straight 10s
		"req: 10\nsec: 10\nmaint: 10", "req: 10\nsec: 10\nmaint: 10",
		// lf_judge #2 (green tests)
		"req: 9\nsec: 9\nmaint: 9", "req: 9\nsec: 9\nmaint: 9",
	}}
	session, st := newStageHarness(t, caller)

	base := map[string]any{
		"project_id": "repo", "slug": "sl", "task_id": "01", "task_slug": "auth",
		"changed_files": "// file: a.go\npackage a",
	}

	// Review with brief-as-data: creates manifest entry + plan.md.
	rev := map[string]any{}
	for k, v := range base {
		rev[k] = v
	}
	rev["brief"] = "Build the auth middleware."
	out := callStage(t, session, "lf_review", rev)
	if out["ok"] != true || out["minor"] != float64(1) {
		t.Fatalf("lf_review = %v", out)
	}
	if brief, err := st.ReadArtifact("repo", "sl", "tasks/01-auth/plan.md"); err != nil || string(brief) != "Build the auth middleware." {
		t.Fatalf("brief-as-data not persisted: %q %v", brief, err)
	}

	// Red tests: judges say 10/10/10, gate says FAIL. PASS must be impossible.
	jd := map[string]any{}
	for k, v := range base {
		jd[k] = v
	}
	jd["test_report"] = map[string]any{"command": "go test", "exit_code": 1, "summary": "1 failed"}
	out = callStage(t, session, "lf_judge", jd)
	if out["ok"] != true || out["verdict"] != "FAIL" || out["avg"] != float64(10) {
		t.Fatalf("red-test judge = %v", out)
	}
	if !strings.Contains(out["gate_reason"].(string), "failing tests force FAIL") {
		t.Fatalf("gate_reason = %v", out["gate_reason"])
	}

	// Green tests: PASS, artifacts returned as data.
	jd["test_report"] = map[string]any{"command": "go test", "exit_code": 0, "summary": "all pass"}
	out = callStage(t, session, "lf_judge", jd)
	if out["ok"] != true || out["verdict"] != "PASS" || out["avg"] != float64(9) {
		t.Fatalf("green-test judge = %v", out)
	}
	if !strings.Contains(out["verdict_md"].(string), "=== JUDGE VERDICT") {
		t.Fatal("verdict_md not returned as data")
	}

	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].Status != "judged:PASS" {
		t.Fatalf("manifest = %+v", m.Tasks[0])
	}
}

// TestJudgeRetryLedger proves ADR-007's guarantee: two judge rounds run, the
// third escalates to a human WITHOUT invoking judges.
func TestJudgeRetryLedger(t *testing.T) {
	// Enough responses for 2 rounds × 2 judges; a 3rd round must not consume any.
	caller := &scriptedCaller{responses: []string{
		"req: 7\nsec: 7\nmaint: 7", "req: 7\nsec: 7\nmaint: 7", // round 1 → FAIL
		"req: 7\nsec: 7\nmaint: 7", "req: 7\nsec: 7\nmaint: 7", // round 2 → FAIL
	}}
	session, st := newStageHarness(t, caller)

	base := map[string]any{
		"project_id": "repo", "slug": "sl", "task_id": "01", "task_slug": "auth",
		"changed_files": "// code", "brief": "Build it.",
		"test_report": map[string]any{"command": "go test", "exit_code": 0},
	}

	for round := 1; round <= 2; round++ {
		out := callStage(t, session, "lf_judge", base)
		if out["ok"] != true || out["escalated"] == true {
			t.Fatalf("round %d should judge: %v", round, out)
		}
		if int(out["attempt"].(float64)) != round {
			t.Fatalf("round %d attempt = %v", round, out["attempt"])
		}
	}

	// Third attempt: escalate_to_human, no model call consumed.
	before := len(caller.responses)
	out := callStage(t, session, "lf_judge", base)
	if out["verdict"] != "escalate_to_human" || out["escalated"] != true {
		t.Fatalf("third attempt must escalate: %v", out)
	}
	if !strings.Contains(out["gate_reason"].(string), "escalates to a human") {
		t.Fatalf("gate_reason = %v", out["gate_reason"])
	}
	if len(caller.responses) != before {
		t.Fatal("escalation must not invoke judges")
	}

	// Ledger persisted in the manifest.
	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].JudgeAttempts != 2 {
		t.Fatalf("judge_attempts = %d, want 2", m.Tasks[0].JudgeAttempts)
	}
}

func TestStageToolsWithoutConfig(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer()
	RegisterStageTools(srv, EngineDeps{Store: st}) // no Cfg
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	defer ts.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "nocfg", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	out := callStage(t, session, "lf_judge", map[string]any{
		"project_id": "r", "slug": "s", "task_id": "01", "task_slug": "t",
		"changed_files": "x", "test_report": map[string]any{"exit_code": 0},
	})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "providers.yaml not loaded") {
		t.Fatalf("no-config error = %v", out)
	}
}

// TestAcceptanceCoverageGateOverMCP: a task with acceptance criteria FAILs
// without coverage (green tests + high scores notwithstanding) and PASSes with
// full coverage — ADR-014, driven over Streamable HTTP.
func TestAcceptanceCoverageGateOverMCP(t *testing.T) {
	caller := &scriptedCaller{responses: []string{
		"req: 9\nsec: 9\nmaint: 9", "req: 9\nsec: 9\nmaint: 9", // round 1
		"req: 9\nsec: 9\nmaint: 9", "req: 9\nsec: 9\nmaint: 9", // round 2
	}}
	session, st := newStageHarness(t, caller)

	// Seed a task whose acceptance.md has two criteria.
	if _, err := st.InitSlug("repo", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest("repo", "sl")
	m.Tasks = append(m.Tasks, store.Task{ID: "01", Slug: "auth", Title: "Auth", Deps: []string{}, Status: "implemented"})
	if err := st.WriteManifest("repo", "sl", m); err != nil {
		t.Fatal(err)
	}
	_ = st.WriteTaskArtifact("repo", "sl", "01", "auth", "plan.md", []byte("brief"))
	_ = st.WriteTaskArtifact("repo", "sl", "01", "auth", "acceptance.md",
		[]byte("- returns hello name\n- 401 when unauthenticated\n"))

	base := map[string]any{
		"project_id": "repo", "slug": "sl", "task_id": "01", "task_slug": "auth",
		"changed_files": "code", "test_report": map[string]any{"command": "go test", "exit_code": 0},
	}

	// No coverage → FAIL, criteria echoed back.
	out := callStage(t, session, "lf_judge", base)
	if out["verdict"] != "FAIL" {
		t.Fatalf("uncovered must FAIL: %v", out)
	}
	crit, _ := out["acceptance_criteria"].([]any)
	if len(crit) != 2 {
		t.Fatalf("criteria not echoed: %v", out["acceptance_criteria"])
	}
	if !strings.Contains(out["gate_reason"].(string), "not attested") {
		t.Fatalf("gate_reason = %v", out["gate_reason"])
	}

	// Full coverage → PASS (attempt 2, still under the ledger cap).
	covered := map[string]any{}
	for k, v := range base {
		covered[k] = v
	}
	covered["acceptance_coverage"] = []any{"TestHello", "TestUnauth"}
	out = callStage(t, session, "lf_judge", covered)
	if out["verdict"] != "PASS" {
		t.Fatalf("full coverage must PASS: %v", out)
	}
}
