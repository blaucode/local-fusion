package plan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestSlugifyMatchesV1(t *testing.T) {
	cases := map[string]string{
		"Add Auth  Middleware": "add-auth-middleware",
		"snake_case_name":      "snake-case-name",
		"Weird!!@#Chars":       "weirdchars",
		"--already-kebab--":    "already-kebab",
		"":                     "untitled",
		"!!!":                  "untitled",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractJSONArray(t *testing.T) {
	if got := extractJSONArray("```json\n[{\"slug\":\"a\"}]\n```"); len(got) != 1 {
		t.Fatalf("fenced: %v", got)
	}
	if got := extractJSONArray("prose [1, 2] trailing"); len(got) != 2 {
		t.Fatalf("bare: %v", got)
	}
	if extractJSONArray("no array here") != nil {
		t.Fatal("garbage must be nil")
	}
	if extractJSONArray("[not json") != nil {
		t.Fatal("bad json must be nil")
	}
}

func TestSplitSections(t *testing.T) {
	adr, plan, acc, found := SplitSections("preamble\n## ADR\nthe adr\n## PLAN\nthe plan\n## ACCEPTANCE\n- item")
	if !found || adr != "the adr" || plan != "the plan" || acc != "- item" {
		t.Fatalf("split = %q %q %q %v", adr, plan, acc, found)
	}
	_, _, _, found = SplitSections("no headers at all")
	if found {
		t.Fatal("no sections must report found=false")
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
  tl-1: {provider: p1, id: v/tl1, roles: [tl], scores: {tl: 9.0}}
pipelines:
  default:
    tl_panel: {n: 1}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{}
	return Deps{Store: st, Cfg: cfg, Caller: fake, Log: func(string) {}}, fake, st
}

var testIntent = store.Intent{Tier: "feature", Ref: "product-docs/PRD.md", ApprovedBy: "adolfo", DraftedBy: "human"}

func TestSoloTwoTasks(t *testing.T) {
	d, fake, st := newDeps(t)
	fake.responses = []response{
		// decompose → two tasks
		{`[{"slug":"api","title":"API endpoint","summary":"Build it","deps":[]},
		   {"slug":"docs","title":"Docs","summary":"Write them","deps":["api"]}]`, true},
		// task 01: frame, explore, compare (with sections)
		{"FRAME1", true}, {"EXPLORE1", true},
		{"## ADR\nadr1\n## PLAN\nplan1\n## ACCEPTANCE\nacc1", true},
		// task 02: compare without sections → plan = whole synthesis
		{"FRAME2", true}, {"EXPLORE2", true}, {"just a plan, no headers", true},
	}

	res, err := Solo(context.Background(), d, nil, "repo", "feat", "Do the thing", "Do the thing\n\n---\nattested\n", "ctx", "", "main", "feature/feat", testIntent, false)
	if err != nil {
		t.Fatal(err)
	}
	m := res.Manifest
	if len(m.Tasks) != 2 || m.Tasks[0].ID != "01" || m.Tasks[1].Slug != "docs" || m.Tasks[1].Deps[0] != "api" {
		t.Fatalf("manifest = %+v", m)
	}
	if m.Intent == nil || m.Intent.ApprovedBy != "adolfo" {
		t.Fatalf("intent not recorded: %+v", m.Intent)
	}

	// 7 calls total: 1 decompose + 2×3 haft; haft chains frame→explore→compare.
	if len(fake.requests) != 7 {
		t.Fatalf("requests = %d", len(fake.requests))
	}
	if !strings.Contains(fake.requests[2].Messages[1].Content, "TASK FRAME:\nFRAME1") {
		t.Fatal("h-explore must receive the frame")
	}
	if !strings.Contains(fake.requests[3].Messages[1].Content, "APPROACHES:\nEXPLORE1") {
		t.Fatal("h-compare must receive frame + explore")
	}

	// Artifacts: sectioned task splits; unsectioned falls back whole.
	plan1, _ := st.ReadArtifact("repo", "feat", "tasks/01-api/plan.md")
	if string(plan1) != "plan1" {
		t.Fatalf("plan1 = %q", plan1)
	}
	adr2, _ := st.ReadArtifact("repo", "feat", "tasks/02-docs/adr.md")
	plan2, _ := st.ReadArtifact("repo", "feat", "tasks/02-docs/plan.md")
	if string(adr2) != "(no fusion; see plan)" || string(plan2) != "just a plan, no headers" {
		t.Fatalf("task2 fallback: adr=%q plan=%q", adr2, plan2)
	}
	scope, _ := st.ReadArtifact("repo", "feat", "scope.md")
	if !strings.Contains(string(scope), "# Scope: feat") || !strings.Contains(string(scope), "**API endpoint** (`api`)") {
		t.Fatalf("scope.md:\n%s", scope)
	}
	reqMD, _ := st.ReadArtifact("repo", "feat", "request.md")
	if !strings.Contains(string(reqMD), "attested") {
		t.Fatal("request.md must carry the attestation block")
	}
}

func TestSoloDecomposeFallback(t *testing.T) {
	d, fake, _ := newDeps(t)
	fake.responses = []response{
		{"I cannot produce JSON today.", true}, // decompose garbage → fallback single task
		{"F", true}, {"E", true}, {"## PLAN\np", true},
	}
	res, err := Solo(context.Background(), d, nil, "repo", "f2", "Fix the login bug\nmore detail", "req", "", "", "main", "feature/f2", testIntent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Manifest.Tasks) != 1 || res.Manifest.Tasks[0].Slug != "fix-the-login-bug" {
		t.Fatalf("fallback manifest = %+v", res.Manifest.Tasks)
	}
}

func TestSoloHaftFailureFailsTask(t *testing.T) {
	d, fake, _ := newDeps(t)
	fake.responses = []response{
		{`[{"slug":"a","title":"A","summary":"s","deps":[]}]`, true},
		{"F", true}, {"", false}, // h-explore dies
	}
	_, err := Solo(context.Background(), d, nil, "repo", "f3", "req", "req", "", "", "main", "b", testIntent, false)
	if err == nil || !strings.Contains(err.Error(), "h-explore stage failed") {
		t.Fatalf("err = %v", err)
	}
}

func newFullDeps(t *testing.T) (Deps, *fakeCaller, *store.Store) {
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
  tl-2: {provider: p1, id: v/tl2, roles: [tl], scores: {tl: 8.5}}
  synth: {provider: p1, id: v/synth, roles: [synthesizer], scores: {}}
pipelines:
  default:
    tl_panel: {n: 2}
    synthesizer: {models: [synth]}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := providers.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{}
	return Deps{Store: st, Cfg: cfg, Caller: fake, Log: func(string) {}}, fake, st
}

func TestFullPanelAndSynthesize(t *testing.T) {
	d, fake, st := newFullDeps(t)
	fake.responses = []response{
		{`[{"slug":"api","title":"API","summary":"s","deps":[]}]`, true},
		{"FRAME", true}, {"EXPLORE", true}, {"COMPARE-OUT", true},
		// panel (tl-1, tl-2)
		{"GAP: missing auth\nSEVERITY: critical\nRESOLUTION: add it", true},
		{"GAP: no tests\nSEVERITY: important\nRESOLUTION: write them", true},
		// synthesizer
		{"## ADR\nfinal adr\n## PLAN\nfinal plan\n## ACCEPTANCE\n- done", true},
	}

	res, err := Full(context.Background(), d, nil, "repo", "feat", "req", "req", "ctx", "", "main", "b", testIntent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Manifest.Tasks) != 1 {
		t.Fatalf("manifest = %+v", res.Manifest)
	}
	if len(fake.requests) != 7 {
		t.Fatalf("calls = %d, want 7 (decompose + 3 haft + 2 panel + synth)", len(fake.requests))
	}
	// Panel prompt carries task + deliberation plan; both panelists get it.
	panelUser := fake.requests[4].Messages[1].Content
	if !strings.Contains(panelUser, "PROPOSED IMPLEMENTATION PLAN:\nCOMPARE-OUT") {
		t.Fatalf("panel prompt:\n%s", panelUser)
	}
	if fake.requests[4].Messages[1].Content != fake.requests[5].Messages[1].Content {
		t.Fatal("panelists must receive the identical prompt")
	}
	// Synthesizer receives findings joined with v1's separator, v1 knobs.
	synthReq := fake.requests[6]
	if synthReq.MaxTokens != 16384 || synthReq.Timeout != 420*time.Second {
		t.Fatalf("synth knobs: %d %v", synthReq.MaxTokens, synthReq.Timeout)
	}
	if !strings.Contains(synthReq.Messages[1].Content, "missing auth\nSEVERITY: critical\nRESOLUTION: add it\n\n---\n\nGAP: no tests") {
		t.Fatalf("findings separator:\n%s", synthReq.Messages[1].Content)
	}
	adr, _ := st.ReadArtifact("repo", "feat", "tasks/01-api/adr.md")
	planMD, _ := st.ReadArtifact("repo", "feat", "tasks/01-api/plan.md")
	if string(adr) != "final adr" || string(planMD) != "final plan" {
		t.Fatalf("artifacts: %q %q", adr, planMD)
	}
}

func TestFullSynthesizerDegradation(t *testing.T) {
	d, fake, st := newFullDeps(t)
	fake.responses = []response{
		{`[{"slug":"api","title":"API","summary":"s","deps":[]}]`, true},
		{"F", true}, {"E", true},
		{"## ADR\ncompare adr\n## PLAN\ncompare plan", true}, // haft compare, sectioned
		{"finding-1", true}, {"", false}, // one panelist dies (skipped)
		{"", false}, // synthesizer dies → degrade
	}
	_, err := Full(context.Background(), d, nil, "repo", "f2", "req", "req", "", "", "main", "b", testIntent, false)
	if err != nil {
		t.Fatal(err)
	}
	// Degradation: artifacts come from the haft compare output.
	adr, _ := st.ReadArtifact("repo", "f2", "tasks/01-api/adr.md")
	planMD, _ := st.ReadArtifact("repo", "f2", "tasks/01-api/plan.md")
	acc, _ := st.ReadArtifact("repo", "f2", "tasks/01-api/acceptance.md")
	if string(adr) != "compare adr" || string(planMD) != "compare plan" || string(acc) != "(synthesis unavailable; see plan)" {
		t.Fatalf("degraded artifacts: %q %q %q", adr, planMD, acc)
	}
}

func TestFullNoSynthesizerConfigured(t *testing.T) {
	d, fake, _ := newDeps(t) // solo config: no synthesizer
	fake.responses = []response{{`[{"slug":"a","title":"A","summary":"s","deps":[]}]`, true}}
	_, err := Full(context.Background(), d, nil, "repo", "f3", "req", "req", "", "", "main", "b", testIntent, false)
	if err == nil || !strings.Contains(err.Error(), "no synthesizer; pass no_fusion=true") {
		t.Fatalf("err = %v", err)
	}
	// v1 fidelity: decompose runs first; the failure costs exactly that one
	// call and never reaches the per-task deliberation.
	if len(fake.requests) != 1 {
		t.Fatalf("calls = %d, want 1 (decompose only)", len(fake.requests))
	}
}

func TestSoloExistingSlugWithoutForce(t *testing.T) {
	d, fake, st := newDeps(t)
	if _, err := st.InitSlug("repo", "f4", "old", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	fake.responses = nil
	if _, err := Solo(context.Background(), d, nil, "repo", "f4", "req", "req", "", "", "main", "b", testIntent, false); err == nil {
		t.Fatal("existing slug without force must error (v1 init_slug semantics)")
	}
}
