package judge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// fakeCaller is the replay-harness seam (ADR-010): it records every
// CallRequest (request parity) and plays canned responses (artifact parity).
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

func TestPyRound2BankersRounding(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{8.125, 8.12}, // exact half, even neighbor down (Python: 8.12)
		{8.375, 8.38}, // exact half, even neighbor up
		{25.0 / 3, 8.33},
		{26.0 / 3, 8.67},
		{9.0, 9.0},
	}
	for _, c := range cases {
		if got := pyRound2(c.in); got != c.want {
			t.Errorf("pyRound2(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseScores(t *testing.T) {
	// Markdown noise, reasoning preamble, restated scores at the end (last wins).
	text := "Let me think... req: 3\nblah\n" +
		"**req**: 9\n> sec: 8.5\n- maint: 9\n" +
		"verdict: PASS\nnotes: Solid work.\nSecond line of notes."
	got := ParseScores(text)
	if got == nil {
		t.Fatal("parse failed")
	}
	if got.Req != 9 || got.Sec != 8.5 || got.Maint != 9 {
		t.Fatalf("scores = %+v", got)
	}
	if got.Avg != 8.83 || got.Verdict != "PASS" {
		t.Fatalf("avg/verdict = %v %s", got.Avg, got.Verdict)
	}
	if !strings.Contains(got.Notes, "Second line") { // DOTALL notes capture
		t.Fatalf("notes = %q", got.Notes)
	}

	if ParseScores("req: 9\nsec: 8") != nil {
		t.Fatal("missing maint must return nil")
	}
	if ParseScores("") != nil {
		t.Fatal("empty must return nil")
	}
	// Derived verdict ignores the model's own claim.
	low := ParseScores("req: 5\nsec: 5\nmaint: 5\nverdict: PASS")
	if low.Verdict != "FAIL" {
		t.Fatal("verdict must derive from avg, not the model's claim")
	}
}

func TestNormalizeTestReport(t *testing.T) {
	if r, err := NormalizeTestReport(nil); r != nil || err != nil {
		t.Fatal("nil → nil, nil")
	}
	if r, err := NormalizeTestReport(""); r != nil || err != nil {
		t.Fatal("empty string → nil, nil")
	}
	if r, err := NormalizeTestReport(map[string]any{}); r != nil || err != nil {
		t.Fatal("empty map → nil, nil")
	}
	r, err := NormalizeTestReport(`{"command":"go test","exit_code":1,"summary":"2 failed"}`)
	if err != nil || r.ExitCode != 1 || r.Command != "go test" {
		t.Fatalf("json string: %+v %v", r, err)
	}
	r, err = NormalizeTestReport(map[string]any{"exit_code": "0"})
	if err != nil || r.ExitCode != 0 {
		t.Fatalf("string exit_code: %+v %v", r, err)
	}
	if _, err := NormalizeTestReport("not json"); err == nil {
		t.Fatal("invalid JSON must error")
	}
	if _, err := NormalizeTestReport(map[string]any{"command": "x"}); err == nil {
		t.Fatal("missing exit_code must error")
	}
	if _, err := NormalizeTestReport(map[string]any{"exit_code": "abc"}); err == nil {
		t.Fatal("non-integer exit_code must error")
	}
	long, err := NormalizeTestReport(map[string]any{"exit_code": 0, "summary": strings.Repeat("é", 3000)})
	if err != nil || len([]rune(long.Summary)) != 2000 {
		t.Fatalf("summary truncation: %d runes", len([]rune(long.Summary)))
	}
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
  ja: {provider: p1, id: v/ja, roles: [judge], scores: {judge: 9.8}}
  jb: {provider: p1, id: v/jb, roles: [judge], scores: {judge: 9.5}}
pipelines:
  default:
    judges: {models: [ja, jb]}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{}
	return Deps{Store: st, Cfg: cfg, Caller: fake, User: "adolfo", ServerVersion: "2.0.0-test"}, fake, st
}

func seedTask(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := st.InitSlug("repo", "sl", "req", "main", "feature/sl", false); err != nil {
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

func TestJudgeTaskGreenPath(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.responses = []response{
		{"req: 9\nsec: 8\nmaint: 9\nverdict: PASS\nnotes: good", true},
		{"req: 9\nsec: 9\nmaint: 9\nverdict: PASS\nnotes: fine", true},
	}

	agg, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "THE CODE", "", "", map[string]any{"command": "go test", "exit_code": 0, "summary": "all green"})
	if err != nil {
		t.Fatal(err)
	}
	// 8.84 verified against CPython: round((round(26/3,2)+9.0)/2, 2) == 8.84
	// (the binary double at 8.835 rounds half-even up).
	if agg.Verdict != "PASS" || agg.Avg != 8.84 {
		t.Fatalf("agg = %+v", agg)
	}

	// Request parity: two sequential judge calls, frozen prompt wording,
	// v1 knobs (max_tokens 32768, timeout 420s).
	if len(fake.requests) != 2 || fake.requests[0].ModelKey != "ja" || fake.requests[1].ModelKey != "jb" {
		t.Fatalf("requests = %+v", fake.requests)
	}
	r0 := fake.requests[0]
	if r0.MaxTokens != 32768 || r0.Timeout != 420*time.Second {
		t.Fatalf("knobs = %d %v", r0.MaxTokens, r0.Timeout)
	}
	if r0.Messages[0].Content != "You are a senior technical judge evaluating a software implementation. Score objectively on a 1-10 scale." {
		t.Fatalf("system prompt drifted: %q", r0.Messages[0].Content)
	}
	user := r0.Messages[1].Content
	for _, want := range []string{
		"IMPLEMENTATION BRIEF (specification):\nTHE BRIEF\n\n",
		"IMPLEMENTATION (code to evaluate):\nTHE CODE\n\n",
		"TEST REPORT (deterministic evidence from the test runner, not a model):\nstatus: GREEN (all passing)\ncommand: go test\nsummary: all green\n\n",
		"PASS if the average of the three scores is >= 8.0, FAIL otherwise.",
	} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q\n---\n%s", want, user)
		}
	}

	// Artifact parity: verdict.md byte shape (python float repr and all).
	verdict, err := st.ReadArtifact("repo", "sl", "build/01-auth/verdict.md")
	if err != nil {
		t.Fatal(err)
	}
	want := `=== JUDGE VERDICT (dual-judge, 2 models) ===

AGGREGATE
req:   9.0
sec:   8.5
maint: 9.0
avg:   8.84
verdict: PASS
tests: GREEN  (go test)

--- ja ---
req:9.0 sec:8.0 maint:9.0 avg:8.67 → PASS
Notes: good

--- jb ---
req:9.0 sec:9.0 maint:9.0 avg:9.0 → PASS
Notes: fine
`
	if string(verdict) != want {
		t.Fatalf("verdict.md diverges:\n--- got ---\n%s\n--- want ---\n%s", verdict, want)
	}

	// Manifest updated: status judged:PASS, ordered scores.
	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].Status != "judged:PASS" || m.Tasks[0].Scores == nil || float64(m.Tasks[0].Scores.Avg) != 8.84 {
		t.Fatalf("manifest task = %+v", m.Tasks[0])
	}

	// Metrics: one build-2.0 line with the new fields.
	data, err := os.ReadFile(filepath.Join(st.Root(), "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["schema_version"] != "build-2.0" || rec["user"] != "adolfo" || rec["repo"] != "repo" || rec["server_version"] != "2.0.0-test" || rec["tests_green"] != true {
		t.Fatalf("metrics = %v", rec)
	}
}

// TestRedTestsForceFail is the M2 exit-gate property: a red test report makes
// PASS impossible no matter what the judges say.
func TestRedTestsForceFail(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.responses = []response{
		{"req: 10\nsec: 10\nmaint: 10\nverdict: PASS", true},
		{"req: 10\nsec: 10\nmaint: 10\nverdict: PASS", true},
	}
	agg, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "", "", map[string]any{"command": "go test", "exit_code": 2})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Verdict != "FAIL" || agg.Avg != 10.0 {
		t.Fatalf("red tests did not force FAIL: %+v", agg)
	}
	if !strings.Contains(agg.GateReason, "'go test' exited 2") {
		t.Fatalf("gate reason = %q", agg.GateReason)
	}
	verdict, _ := st.ReadArtifact("repo", "sl", "build/01-auth/verdict.md")
	if !strings.Contains(string(verdict), "tests: RED (exit 2)  (go test)") ||
		!strings.Contains(string(verdict), "gate: test gate:") {
		t.Fatalf("verdict.md missing gate lines:\n%s", verdict)
	}
	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].Status != "judged:FAIL" {
		t.Fatalf("manifest status = %s", m.Tasks[0].Status)
	}
}

func TestJudgeRetryAndDegradation(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	// Judge 1: first call fails, retry succeeds. Judge 2: both fail → skipped.
	fake.responses = []response{
		{"", false},
		{"req: 9\nsec: 9\nmaint: 9", true},
		{"", false},
		{"", false},
	}
	agg, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agg.Judges) != 1 || agg.Judges[0].ModelKey != "ja" {
		t.Fatalf("degradation: %+v", agg.Judges)
	}
	if len(fake.requests) != 4 || !strings.HasSuffix(fake.requests[1].Label, "(retry)") {
		t.Fatalf("retry labels: %+v", fake.requests)
	}
	// All judges fail → error, never a fabricated verdict.
	fake.responses = []response{{"", false}, {"", false}, {"", false}, {"", false}}
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "", "", nil); err == nil {
		t.Fatal("no parseable judges must error")
	}
}

func TestJudgeInputValidation(t *testing.T) {
	d, _, st := newDeps(t)
	seedTask(t, st)
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "  ", "", "", nil); err == nil {
		t.Fatal("empty implementation must error")
	}
	if _, err := Task(context.Background(), d, "repo", "sl", "99", "auth", "CODE", "", "", nil); err == nil {
		t.Fatal("missing brief must error")
	}
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CODE", "", "", "not json"); err == nil {
		t.Fatal("malformed test_report must fail fast")
	}
}
