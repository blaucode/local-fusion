package coder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// fakeCaller routes by label prefix so the parallel coders are deterministic.
type fakeCaller struct {
	mu       sync.Mutex
	requests []providers.CallRequest
	byLabel  map[string]response // key: first token of label ("coder-a", "evaluator", ...)
}

type response struct {
	content string
	ok      bool
}

func (f *fakeCaller) CallModel(_ context.Context, req providers.CallRequest) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	key := strings.SplitN(req.Label, " ", 2)[0]
	r, ok := f.byLabel[key]
	if !ok {
		return "", false
	}
	return r.content, r.ok
}

const twoBlocks = "prose before\n" +
	"<<<FILE src/a.go>>>\npackage a\n<<<END>>>\n" +
	"between\n" +
	"<<<FILE docs/readme.md>>>\n# hi\n<<<END>>>\ntrailing"

func TestParseFileBlocks(t *testing.T) {
	files, err := ParseFileBlocks(twoBlocks)
	if err != nil || len(files) != 2 {
		t.Fatalf("parse: %v %v", files, err)
	}
	if files[0].Path != "src/a.go" || files[0].Content != "package a" {
		t.Fatalf("file0 = %+v", files[0])
	}
	if files[1].Path != "docs/readme.md" || files[1].Content != "# hi" {
		t.Fatalf("file1 = %+v", files[1])
	}
	// Traversal path fails the whole parse (v1 raises).
	if _, err := ParseFileBlocks("<<<FILE ../evil>>>\nx\n<<<END>>>"); err == nil {
		t.Fatal("traversal path must error")
	}
	// No blocks → empty, nil error.
	if files, err := ParseFileBlocks("no blocks here"); err != nil || len(files) != 0 {
		t.Fatalf("no blocks: %v %v", files, err)
	}
	// Malformed opener must not swallow the file (the path is single-line).
	if files, _ := ParseFileBlocks("<<<FILE bad\nmore\n<<<END>>>"); len(files) != 0 {
		t.Fatalf("malformed opener parsed: %+v", files)
	}
}

func TestParseBase(t *testing.T) {
	log := func(string) {}
	if ParseBase("RATIONALE: x\nBASE: B\nGRAFTS:", log) != "B" {
		t.Fatal("BASE: B")
	}
	if ParseBase("  base: a\n", log) != "A" {
		t.Fatal("case-insensitive")
	}
	if ParseBase("no base line", log) != "A" || ParseBase("", log) != "A" {
		t.Fatal("default A")
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
  ca: {provider: p1, id: v/ca, roles: [coder]}
  cb: {provider: p1, id: v/cb, roles: [coder]}
  ev: {provider: p1, id: v/ev, roles: [judge]}
  ld: {provider: p1, id: v/ld, roles: [synthesizer]}
pipelines:
  default:
    coder_fusion: {coder_a: ca, coder_b: cb, evaluator: ev, lead: ld}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{byLabel: map[string]response{}}
	return Deps{Store: st, Cfg: cfg, Caller: fake, Log: func(string) {}, User: "t", ServerVersion: "test"}, fake, st
}

func seedTask(t *testing.T, st *store.Store) {
	t.Helper()
	if _, err := st.InitSlug("repo", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest("repo", "sl")
	m.Tasks = append(m.Tasks, store.Task{ID: "01", Slug: "auth", Title: "Auth", Deps: []string{}, Status: "planned"})
	if err := st.WriteManifest("repo", "sl", m); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"plan.md": "THE PLAN", "acceptance.md": "THE ACCEPTANCE"} {
		if err := st.WriteTaskArtifact("repo", "sl", "01", "auth", name, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
}

func fileBlock(path, content string) string {
	return "<<<FILE " + path + ">>>\n" + content + "\n<<<END>>>"
}

func TestSoloPath(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.byLabel["coder-a"] = response{fileBlock("src/x.go", "package x"), true}

	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "CTX", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseChosen != "solo" || res.Notes != "solo coder" || len(res.Files) != 1 {
		t.Fatalf("res = %+v", res)
	}

	// v1 knobs + prompt assembly: plan/acceptance/context + EMIT_INSTRUCTIONS.
	req := fake.requests[0]
	if req.MaxTokens != 16384 || req.Timeout.Seconds() != 420 {
		t.Fatalf("knobs = %d %v", req.MaxTokens, req.Timeout)
	}
	p := req.Messages[0].Content
	for _, want := range []string{
		"You are a senior engineer. Implement this task.\n\n<plan>\nTHE PLAN\n</plan>\n\n",
		"<acceptance>\nTHE ACCEPTANCE\n</acceptance>\n\n<context>\nCTX\n</context>\n\n",
		"Emit every file you create or modify as a FILE block",
		"text outside blocks is ignored by the parser.",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("coder prompt missing %q:\n%s", want, p)
		}
	}

	got, err := st.ReadArtifact("repo", "sl", "build/01-auth/proposed/src/x.go")
	if err != nil || string(got) != "package x" {
		t.Fatalf("proposed file: %q %v", got, err)
	}
	m, _ := st.ReadManifest("repo", "sl")
	if m.Tasks[0].Status != "implemented" {
		t.Fatalf("status = %s", m.Tasks[0].Status)
	}
}

func TestFusionHappyPath(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.byLabel["coder-a"] = response{fileBlock("a.go", "A SOLUTION"), true}
	fake.byLabel["coder-b"] = response{fileBlock("b.go", "B SOLUTION"), true}
	fake.byLabel["evaluator"] = response{"BASE: B\nRATIONALE: b better\nGRAFTS:\n- take a's error handling", true}
	fake.byLabel["lead"] = response{fileBlock("final.go", "MERGED"), true}

	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseChosen != "B" || res.Notes != "base B + 1 graft(s) via lead" {
		t.Fatalf("res = %+v", res)
	}
	if res.Files[0].Path != "final.go" {
		t.Fatalf("files = %+v", res.Files)
	}
	// Lead must receive base B's solution and the evaluator text, never sol A wholesale.
	var leadPrompt string
	for _, r := range fake.requests {
		if r.Label == "lead" {
			leadPrompt = r.Messages[0].Content
		}
	}
	if !strings.Contains(leadPrompt, "<base_solution>\n"+fileBlock("b.go", "B SOLUTION")) ||
		!strings.Contains(leadPrompt, "take a's error handling") {
		t.Fatalf("lead prompt:\n%s", leadPrompt)
	}
}

func TestFusionSurvivorDegradation(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	fake.byLabel["coder-b"] = response{fileBlock("b.go", "B"), true} // coder-a absent → fails+retry fails

	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseChosen != "B" || !strings.Contains(res.Notes, "degraded: coder-a failed, used B directly") {
		t.Fatalf("res = %+v", res)
	}
	got, _ := st.ReadArtifact("repo", "sl", "build/01-auth/proposed/b.go")
	if string(got) != "B" {
		t.Fatalf("survivor file: %q", got)
	}
}

func TestFusionEvaluatorAndLeadDegradation(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	// Both coders fine; evaluator fails → BASE A, no grafts; lead fails →
	// fall back to base A's files.
	fake.byLabel["coder-a"] = response{fileBlock("a.go", "A"), true}
	fake.byLabel["coder-b"] = response{fileBlock("b.go", "B"), true}

	res, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseChosen != "A" || !strings.Contains(res.Notes, "lead failed; used base without grafts") {
		t.Fatalf("res = %+v", res)
	}
	if res.Files[0].Path != "a.go" {
		t.Fatalf("files = %+v", res.Files)
	}
	if got, _ := st.ReadArtifact("repo", "sl", "build/01-auth/proposed/a.go"); string(got) != "A" {
		t.Fatalf("proposed = %q", got)
	}
}

func TestFusionBothCodersFail(t *testing.T) {
	d, fake, st := newDeps(t)
	seedTask(t, st)
	_ = fake
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "", "", false); err == nil ||
		!strings.Contains(err.Error(), "both coders failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingBriefErrors(t *testing.T) {
	d, _, st := newDeps(t)
	if _, err := st.InitSlug("repo", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Task(context.Background(), d, "repo", "sl", "01", "auth", "", "", true); err == nil ||
		!strings.Contains(err.Error(), "run planning first") {
		t.Fatalf("err = %v", err)
	}
}
