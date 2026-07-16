//go:build replay

// TestParity is the deterministic record/replay parity gate (ADR-010):
// recorded v1 requests must match the Go engine's requests call-for-call
// (request parity), and the artifacts produced from the recorded responses
// must be byte-identical to what v1 wrote (artifact parity).
//
// Record half: scripts/record-v1.py (v1 checkout, host-side, LF_RECORD hook).
// Fixtures: internal/engine/testdata/parity/<case>/.
package engine_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"local-fusion/internal/engine/coder"
	"local-fusion/internal/engine/judge"
	"local-fusion/internal/engine/plan"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/engine/review"
	"local-fusion/internal/store"
)

type recordedCall struct {
	ModelID   string              `json:"model_id"`
	BaseURL   string              `json:"base_url"`
	Messages  []providers.Message `json:"messages"`
	MaxTokens int                 `json:"max_tokens"`
	Timeout   float64             `json:"timeout"` // seconds, v1 int
	Content   string              `json:"content"`
	// Failed marks a recorder-injected failure (degradation-path fixtures):
	// the replay verifies the request, then reports the call as failed.
	Failed bool `json:"failed,omitempty"`
}

// replayCaller asserts request parity per call and plays the recorded response.
type replayCaller struct {
	t     *testing.T
	calls []recordedCall
	next  int
}

func (rc *replayCaller) CallModel(_ context.Context, req providers.CallRequest) (string, bool) {
	rc.t.Helper()
	if rc.next >= len(rc.calls) {
		rc.t.Fatalf("request parity: Go engine made call %d but v1 recorded only %d", rc.next+1, len(rc.calls))
	}
	rec := rc.calls[rc.next]
	rc.next++

	if req.ModelID != rec.ModelID {
		rc.t.Fatalf("call %d: model %q, v1 sent %q", rc.next, req.ModelID, rec.ModelID)
	}
	if req.BaseURL != rec.BaseURL {
		rc.t.Fatalf("call %d: base_url %q, v1 sent %q", rc.next, req.BaseURL, rec.BaseURL)
	}
	if req.MaxTokens != rec.MaxTokens {
		rc.t.Fatalf("call %d: max_tokens %d, v1 sent %d", rc.next, req.MaxTokens, rec.MaxTokens)
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 190 * time.Second // client default, mirrors v1 call_model
	}
	if timeout.Seconds() != rec.Timeout {
		rc.t.Fatalf("call %d: timeout %v, v1 sent %vs", rc.next, timeout, rec.Timeout)
	}
	if len(req.Messages) != len(rec.Messages) {
		rc.t.Fatalf("call %d: %d messages, v1 sent %d", rc.next, len(req.Messages), len(rec.Messages))
	}
	for i := range req.Messages {
		if req.Messages[i].Role != rec.Messages[i].Role {
			rc.t.Fatalf("call %d message %d: role %q vs v1 %q", rc.next, i, req.Messages[i].Role, rec.Messages[i].Role)
		}
		if req.Messages[i].Content != rec.Messages[i].Content {
			rc.t.Fatalf("call %d message %d (%s): content diverges from v1:\n--- go ---\n%s\n--- v1 ---\n%s",
				rc.next, i, req.Messages[i].Role, req.Messages[i].Content, rec.Messages[i].Content)
		}
	}
	if rec.Failed {
		return "", false
	}
	return rec.Content, true
}

// callMatches reports whether a Go request equals a recorded v1 call on every
// parity-relevant field. Used by the pool caller to match order-independently.
func callMatches(req providers.CallRequest, rec recordedCall) bool {
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 190 * time.Second // client default, mirrors v1 call_model
	}
	if req.ModelID != rec.ModelID || req.BaseURL != rec.BaseURL ||
		req.MaxTokens != rec.MaxTokens || timeout.Seconds() != rec.Timeout ||
		len(req.Messages) != len(rec.Messages) {
		return false
	}
	for i := range req.Messages {
		if req.Messages[i].Role != rec.Messages[i].Role ||
			req.Messages[i].Content != rec.Messages[i].Content {
			return false
		}
	}
	return true
}

// poolReplayCaller matches each Go request against ANY unconsumed recorded
// call (order-tolerant) and is safe under concurrent calls. It exists for the
// coder-fusion path, where v1 runs coder-a and coder-b in parallel threads so
// their recording order is non-deterministic. Sequential stages still match
// uniquely by content, so the pool never mis-pairs them.
type poolReplayCaller struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []recordedCall
	consumed []bool
}

func (rc *poolReplayCaller) CallModel(_ context.Context, req providers.CallRequest) (string, bool) {
	rc.t.Helper()
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for i := range rc.calls {
		if rc.consumed[i] || !callMatches(req, rc.calls[i]) {
			continue
		}
		rc.consumed[i] = true
		if rc.calls[i].Failed {
			return "", false
		}
		return rc.calls[i].Content, true
	}
	rc.t.Fatalf("request parity: Go request has no matching v1 recording (model=%s max_tokens=%d, %d messages)\n--- first message ---\n%s",
		req.ModelID, req.MaxTokens, len(req.Messages), firstContent(req))
	return "", false
}

func (rc *poolReplayCaller) unconsumed() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	n := 0
	for _, c := range rc.consumed {
		if !c {
			n++
		}
	}
	return n
}

func firstContent(req providers.CallRequest) string {
	if len(req.Messages) == 0 {
		return "(no messages)"
	}
	return req.Messages[0].Content
}

func loadRecording(t *testing.T, path string) []recordedCall {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("no recording — run scripts/record-v1.py first: %v", err)
	}
	defer f.Close()
	var calls []recordedCall
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var call recordedCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func TestParityHexcolor(t *testing.T) {
	dir := filepath.Join("testdata", "parity", "hexcolor")
	brief := readFile(t, filepath.Join(dir, "brief.md"))
	changed := readFile(t, filepath.Join(dir, "changed_files.txt"))
	var testReport map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "test_report.json"))), &testReport); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		t.Fatalf("no recording — run scripts/record-v1.py first: %v", err)
	}
	defer f.Close()
	rc := &replayCaller{t: t}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var call recordedCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		rc.calls = append(rc.calls, call)
	}

	cfg, err := providers.Load(filepath.Join(dir, "providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Seed the store exactly as scripts/record-v1.py seeded the v1 project.
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const projectID, slug = "parity", "hexcolor"
	if _, err := st.InitSlug(projectID, slug, brief, "main", "feature/hexcolor", false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest(projectID, slug)
	m.Tasks = append(m.Tasks, store.Task{ID: "01", Slug: "parse", Title: "parse", Deps: []string{}, Status: "planned"})
	if err := st.WriteManifest(projectID, slug, m); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTaskArtifacts(projectID, slug, "01", "parse", "", brief, "", ""); err != nil {
		t.Fatal(err)
	}

	log := func(s string) { t.Log(s) }

	if _, err := review.Task(context.Background(),
		review.Deps{Store: st, Cfg: cfg, Caller: rc, Log: log},
		projectID, slug, "01", "parse", changed, "default"); err != nil {
		t.Fatalf("review: %v", err)
	}
	if _, err := judge.Task(context.Background(),
		judge.Deps{Store: st, Cfg: cfg, Caller: rc, Log: log, User: "parity", ServerVersion: "parity"},
		projectID, slug, "01", "parse", changed, "default", "", testReport, nil); err != nil {
		t.Fatalf("judge: %v", err)
	}

	if rc.next != len(rc.calls) {
		t.Fatalf("request parity: v1 recorded %d calls, Go engine made %d", len(rc.calls), rc.next)
	}

	// Artifact parity: byte-identical to what v1 wrote from the same responses.
	for _, artifact := range []string{"build/01-parse/review.md", "build/01-parse/verdict.md", "manifest.json"} {
		got, err := st.ReadArtifact(projectID, slug, artifact)
		if err != nil {
			t.Fatalf("%s: %v", artifact, err)
		}
		want, err := os.ReadFile(filepath.Join(dir, "artifacts", filepath.FromSlash(artifact)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("artifact parity: %s diverges from v1\n--- go ---\n%s\n--- v1 ---\n%s", artifact, got, want)
		}
	}
	fmt.Println("parity: 5-call request parity + 3-artifact byte parity vs v1 recording")
}

// TestParityPlanSolo / PlanFull / PlanFullDegraded replay recorded v1
// plan_feature runs through the Go plan engine. The manifest is compared
// modulo the additive v2 `intent` field (ADR-011 — documented addition; v2
// adds fields, never changes existing ones); every other artifact must be
// byte-identical. The degraded case replays a recorder-injected synthesizer
// failure (ADR-010's injected-failure degradation path).
func TestParityPlanSolo(t *testing.T) { runPlanParity(t, "plan-solo", plan.Solo) }

func TestParityPlanFull(t *testing.T) { runPlanParity(t, "plan-full", plan.Full) }

func TestParityPlanFullDegraded(t *testing.T) { runPlanParity(t, "plan-full-degraded", plan.Full) }

type planFn func(context.Context, plan.Deps, plan.Progress,
	string, string, string, string, string, string, string, string,
	store.Intent, bool) (plan.SoloResult, error)

func runPlanParity(t *testing.T, caseName string, run planFn) {
	dir := filepath.Join("testdata", "parity", caseName)
	request := readFile(t, filepath.Join(dir, "request.txt"))
	codeContext := readFile(t, filepath.Join(dir, "context.txt"))
	slug := strings.TrimSpace(readFile(t, filepath.Join(dir, "slug.txt")))

	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		t.Fatalf("no recording — run scripts/record-v1.py … %s first: %v", caseName, err)
	}
	defer f.Close()
	rc := &replayCaller{t: t}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var call recordedCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		rc.calls = append(rc.calls, call)
	}

	cfg, err := providers.Load(filepath.Join("testdata", "parity", "hexcolor", "providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const projectID = "parity"
	intent := store.Intent{Tier: "fix", Ref: "parity", ApprovedBy: "parity", DraftedBy: "human"}
	// requestMD = plain request text: v1 init_slug wrote exactly that, and
	// this test proves engine parity (the attestation block is tool-layer).
	if _, err := run(context.Background(),
		plan.Deps{Store: st, Cfg: cfg, Caller: rc, Log: func(s string) { t.Log(s) }},
		nil, projectID, slug, request, request, codeContext, "default",
		"main", "feature/"+slug, intent, false); err != nil {
		t.Fatalf("plan %s: %v", caseName, err)
	}

	if rc.next != len(rc.calls) {
		t.Fatalf("request parity: v1 recorded %d calls, Go engine made %d", len(rc.calls), rc.next)
	}

	// Walk everything v1 wrote and demand byte equality (manifest: see above).
	root := filepath.Join(dir, "artifacts")
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		relSlash := filepath.ToSlash(rel)
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if relSlash == "manifest.json" {
			m, rerr := st.ReadManifest(projectID, slug)
			if rerr != nil {
				return rerr
			}
			m.Intent = nil
			got, merr := store.MarshalManifest(m)
			if merr != nil {
				return merr
			}
			if !bytes.Equal(got, want) {
				t.Errorf("artifact parity: manifest.json (modulo intent) diverges\n--- go ---\n%s\n--- v1 ---\n%s", got, want)
			}
			return nil
		}
		got, gerr := st.ReadArtifact(projectID, slug, relSlash)
		if gerr != nil {
			t.Errorf("artifact parity: %s missing in Go store: %v", relSlash, gerr)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("artifact parity: %s diverges from v1\n--- go ---\n%s\n--- v1 ---\n%s", relSlash, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("parity: %s %d-call request parity + artifact tree byte parity vs v1 recording\n", caseName, len(rc.calls))
}

// TestParityCoderSolo replays the recorded v1 coder_fusion_task(solo=True)
// run: request parity for the coder call(s) and byte parity for the proposed
// files + manifest (status implemented).
func TestParityCoderSolo(t *testing.T) {
	dir := filepath.Join("testdata", "parity", "coder-solo")
	planMD := readFile(t, filepath.Join(dir, "plan.md"))
	acceptance := readFile(t, filepath.Join(dir, "acceptance.md"))
	codeContext := readFile(t, filepath.Join(dir, "context.txt"))
	slug := strings.TrimSpace(readFile(t, filepath.Join(dir, "slug.txt")))

	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		t.Fatalf("no recording — run scripts/record-v1.py … coder-solo first: %v", err)
	}
	defer f.Close()
	rc := &replayCaller{t: t}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var call recordedCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		rc.calls = append(rc.calls, call)
	}

	cfg, err := providers.Load(filepath.Join("testdata", "parity", "hexcolor", "providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Seed exactly as the recorder seeded v1.
	const projectID = "parity"
	if _, err := st.InitSlug(projectID, slug, "recorded coder parity case", "main", "feature/"+slug, false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest(projectID, slug)
	m.Tasks = []store.Task{{ID: "01", Slug: "impl", Title: "impl", Deps: []string{}, Status: "planned"}}
	if err := st.WriteManifest(projectID, slug, m); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTaskArtifacts(projectID, slug, "01", "impl", "", planMD, acceptance, codeContext); err != nil {
		t.Fatal(err)
	}

	res, err := coder.Task(context.Background(),
		coder.Deps{Store: st, Cfg: cfg, Caller: rc, Log: func(s string) { t.Log(s) },
			User: "parity", ServerVersion: "parity"},
		projectID, slug, "01", "impl", codeContext, "default", true)
	if err != nil {
		t.Fatalf("coder solo: %v", err)
	}
	if res.BaseChosen != "solo" {
		t.Fatalf("res = %+v", res)
	}

	if rc.next != len(rc.calls) {
		t.Fatalf("request parity: v1 recorded %d calls, Go engine made %d", len(rc.calls), rc.next)
	}

	// Artifact parity across everything v1 wrote (incl. build/…/proposed/**).
	root := filepath.Join(dir, "artifacts")
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		relSlash := filepath.ToSlash(rel)
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, gerr := st.ReadArtifact(projectID, slug, relSlash)
		if gerr != nil {
			t.Errorf("artifact parity: %s missing in Go store: %v", relSlash, gerr)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("artifact parity: %s diverges from v1\n--- go ---\n%s\n--- v1 ---\n%s", relSlash, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("parity: coder-solo %d-call request parity + artifact tree byte parity vs v1 recording\n", len(rc.calls))
}

// TestParityCoderFusion / CoderFusionDegraded replay recorded v1
// coder_fusion_task(solo=False) runs through the Go fusion path. The two coder
// calls run in parallel in v1 (non-deterministic recording order), so the
// replay uses the order-tolerant pool caller; recorded call failures (natural
// timeouts or the injected coder-b failure) are {failed:true} sentinels the
// replay reproduces (ADR-010 degradation paths).
//
// Both fixtures happen to capture degradation rungs — the flaky providers v1
// documents (DeepSeek lead, devstral coder-b) failed during recording:
//   - coder-fusion:          coders + evaluator OK, lead fails → base without
//     grafts (5 calls: 2 coders, evaluator, lead ×2).
//   - coder-fusion-degraded: coder-b fails → survivor A, no evaluator/lead
//     (3 calls: coder-a, coder-b ×2).
//
// Between them they parity-verify parallel dispatch, both degradation rungs,
// evaluator + parse_base, and lead-request construction (the lead sentinel
// only matches if the Go lead request is byte-identical to v1's). The
// all-succeed merge output is covered by the unit tests (coder_test.go).
func TestParityCoderFusion(t *testing.T) { runCoderFusionParity(t, "coder-fusion") }

func TestParityCoderFusionDegraded(t *testing.T) { runCoderFusionParity(t, "coder-fusion-degraded") }

func runCoderFusionParity(t *testing.T, caseName string) {
	dir := filepath.Join("testdata", "parity", caseName)
	planMD := readFile(t, filepath.Join(dir, "plan.md"))
	acceptance := readFile(t, filepath.Join(dir, "acceptance.md"))
	codeContext := readFile(t, filepath.Join(dir, "context.txt"))
	slug := strings.TrimSpace(readFile(t, filepath.Join(dir, "slug.txt")))

	calls := loadRecording(t, filepath.Join(dir, "recording.jsonl"))
	rc := &poolReplayCaller{t: t, calls: calls, consumed: make([]bool, len(calls))}

	cfg, err := providers.Load(filepath.Join("testdata", "parity", "hexcolor", "providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const projectID = "parity"
	if _, err := st.InitSlug(projectID, slug, "recorded coder parity case", "main", "feature/"+slug, false); err != nil {
		t.Fatal(err)
	}
	m, _ := st.ReadManifest(projectID, slug)
	m.Tasks = []store.Task{{ID: "01", Slug: "impl", Title: "impl", Deps: []string{}, Status: "planned"}}
	if err := st.WriteManifest(projectID, slug, m); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTaskArtifacts(projectID, slug, "01", "impl", "", planMD, acceptance, codeContext); err != nil {
		t.Fatal(err)
	}

	if _, err := coder.Task(context.Background(),
		coder.Deps{Store: st, Cfg: cfg, Caller: rc, Log: func(s string) { t.Log(s) },
			User: "parity", ServerVersion: "parity"},
		projectID, slug, "01", "impl", codeContext, "default", false); err != nil {
		t.Fatalf("coder fusion: %v", err)
	}

	if n := rc.unconsumed(); n != 0 {
		t.Fatalf("request parity: %d recorded v1 calls were never made by the Go engine", n)
	}

	root := filepath.Join(dir, "artifacts")
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		relSlash := filepath.ToSlash(rel)
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, gerr := st.ReadArtifact(projectID, slug, relSlash)
		if gerr != nil {
			t.Errorf("artifact parity: %s missing in Go store: %v", relSlash, gerr)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("artifact parity: %s diverges from v1\n--- go ---\n%s\n--- v1 ---\n%s", relSlash, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("parity: %s %d-call request parity (order-tolerant) + artifact tree byte parity vs v1 recording\n", caseName, len(calls))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
