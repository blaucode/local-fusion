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
	"testing"
	"time"

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
	return rec.Content, true
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
		projectID, slug, "01", "parse", changed, "default", "", testReport); err != nil {
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

// TestParityPlanSolo replays the recorded v1 plan_feature(no_fusion=True) run
// through the Go plan-solo engine. The manifest is compared modulo the
// additive v2 `intent` field (ADR-011 — documented addition; v2 adds fields,
// never changes existing ones); every other artifact must be byte-identical.
func TestParityPlanSolo(t *testing.T) {
	dir := filepath.Join("testdata", "parity", "plan-solo")
	request := readFile(t, filepath.Join(dir, "request.txt"))
	codeContext := readFile(t, filepath.Join(dir, "context.txt"))
	slug := strings.TrimSpace(readFile(t, filepath.Join(dir, "slug.txt")))

	f, err := os.Open(filepath.Join(dir, "recording.jsonl"))
	if err != nil {
		t.Fatalf("no recording — run scripts/record-v1.py … plan-solo first: %v", err)
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
	if _, err := plan.Solo(context.Background(),
		plan.Deps{Store: st, Cfg: cfg, Caller: rc, Log: func(s string) { t.Log(s) }},
		nil, projectID, slug, request, request, codeContext, "default",
		"main", "feature/"+slug, intent, false); err != nil {
		t.Fatalf("plan.Solo: %v", err)
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
	fmt.Printf("parity: plan-solo %d-call request parity + artifact tree byte parity vs v1 recording\n", len(rc.calls))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
