package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

type fakeCaller struct {
	requests  []providers.CallRequest
	responses []response
}

type response struct {
	content string
	ok      bool
}

func (f *fakeCaller) CallModel(_ context.Context, req providers.CallRequest) (string, bool) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return "", false
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r.content, r.ok
}

func newDeps(t *testing.T) (Deps, *fakeCaller, *store.Store) {
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
  r1: {provider: p1, id: v/r1, roles: [reviewer], scores: {reviewer: 9.0}}
  r2: {provider: p1, id: v/r2, roles: [reviewer], scores: {reviewer: 8.5}}
pipelines:
  default:
    reviewer_panel: {n: 2}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{}
	return Deps{Store: st, Cfg: cfg, Caller: fake}, fake, st
}

func seedTask(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := st.InitSlug("repo", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest("repo", "sl")
	m.Tasks = append(m.Tasks, store.Task{ID: "01", Slug: "auth", Title: "Auth", Deps: []string{}, Status: "implemented"})
	if err := st.WriteManifest("repo", "sl", m); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTaskArtifact("repo", "sl", "01", "auth", "plan.md", []byte("THE BRIEF")); err != nil {
		t.Fatal(err)
	}
}

func TestCountSeverities(t *testing.T) {
	c, i, m := CountSeverities([]string{
		"FINDING: x\nSEVERITY: critical\n",
		"severity: Important stuff\nSEVERITY: minor\nnot a severity line",
	})
	if c != 1 || i != 1 || m != 1 {
		t.Fatalf("counts = %d %d %d", c, i, m)
	}
}

func TestReviewTaskReportAndManifest(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.responses = []response{
		{"FINDING: missing validation\nSEVERITY: critical\nFILE: a.go\nRESOLUTION: add it", true},
		{"NO DEFECTS FOUND", true},
	}

	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "THE CODE", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Critical != 1 || res.Important != 0 || res.Minor != 0 || len(res.Findings) != 2 {
		t.Fatalf("result = %+v", res)
	}

	// Request parity: v1 defaults (max_tokens 8192, no per-call timeout
	// override), frozen prompt wording.
	if len(fake.requests) != 2 || fake.requests[0].MaxTokens != 8192 || fake.requests[0].Timeout != 0 {
		t.Fatalf("requests = %+v", fake.requests)
	}
	user := fake.requests[0].Messages[1].Content
	if !strings.Contains(user, "IMPLEMENTATION BRIEF (what was supposed to be built):\nTHE BRIEF\n\n") ||
		!strings.HasSuffix(user, "If no defects found, respond with: NO DEFECTS FOUND") {
		t.Fatalf("review prompt drifted:\n%s", user)
	}

	report, err := st.ReadArtifact("repo", "sl", "build/01-auth/review.md")
	if err != nil {
		t.Fatal(err)
	}
	want := `=== CODE REVIEW REPORT ===
Reviewers: 2 models

--- Reviewer 1 (r1) ---
FINDING: missing validation
SEVERITY: critical
FILE: a.go
RESOLUTION: add it

--- Reviewer 2 (r2) ---
NO DEFECTS FOUND

=== SUMMARY ===
Critical: 1  Important: 0  Minor: 0
`
	if string(report) != want {
		t.Fatalf("review.md diverges:\n--- got ---\n%s\n--- want ---\n%s", report, want)
	}

	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].Status != "reviewed" {
		t.Fatalf("manifest status = %s", m.Tasks[0].Status)
	}
}

func TestReviewDegradation(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)

	// One reviewer fails → report from the survivor.
	fake.responses = []response{{"", false}, {"NO DEFECTS FOUND", true}}
	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "")
	if err != nil || len(res.Findings) != 1 || res.Findings[0].ModelKey != "r2" {
		t.Fatalf("degradation: %+v %v", res, err)
	}
	report, _ := st.ReadArtifact("repo", "sl", "build/01-auth/review.md")
	if !strings.Contains(string(report), "Reviewers: 1 models") {
		t.Fatalf("survivor count wrong:\n%s", report)
	}

	// All fail → error.
	fake.responses = []response{{"", false}, {"", false}}
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", ""); err == nil {
		t.Fatal("all reviewers failing must error")
	}
}

func TestReviewInputValidation(t *testing.T) {
	d, _, st := newDeps(t)
	seedTask(t, st)
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", " ", ""); err == nil {
		t.Fatal("empty implementation must error")
	}
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "nope"); err == nil {
		t.Fatal("unknown pipeline must error")
	}
}
