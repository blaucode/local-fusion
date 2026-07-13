package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"local-fusion/internal/jobs"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInitSlugAndManifestRoundTrip(t *testing.T) {
	s := newStore(t)

	m, err := s.InitSlug("myrepo", "vendor-api", "Add auth middleware", "main", "feature/vendor-api", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Slug != "vendor-api" || len(m.Tasks) != 0 {
		t.Fatalf("init manifest = %+v", m)
	}

	// v1 semantics: re-init without force fails; with force succeeds.
	if _, err := s.InitSlug("myrepo", "vendor-api", "x", "main", "b", false); err == nil {
		t.Fatal("re-init without force must fail")
	}
	if _, err := s.InitSlug("myrepo", "vendor-api", "x", "main", "b", true); err != nil {
		t.Fatalf("re-init with force: %v", err)
	}

	m.Tasks = append(m.Tasks, Task{ID: "01", Slug: "auth", Title: "Auth middleware", Deps: []string{}, Status: "planned", Scores: nil})
	if err := s.WriteManifest("myrepo", "vendor-api", m); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadManifest("myrepo", "vendor-api")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Status != "planned" || got.Tasks[0].Scores != nil {
		t.Fatalf("round-trip = %+v", got)
	}
}

// TestManifestShapeMatchesV1 pins the serialized field set and order to
// v1 plan.py's manifest (port contract: schema unchanged).
func TestManifestShapeMatchesV1(t *testing.T) {
	s := newStore(t)
	m := Manifest{
		Slug: "sl", BaseBranch: "main", Branch: "feature/sl",
		Tasks: []Task{{ID: "01", Slug: "t", Title: "T", Deps: []string{}, Status: "planned", Scores: nil}},
	}
	if err := s.WriteManifest("p", "sl", m); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ReadArtifact("p", "sl", "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "slug": "sl",
  "base_branch": "main",
  "branch": "feature/sl",
  "tasks": [
    {
      "id": "01",
      "slug": "t",
      "title": "T",
      "deps": [],
      "status": "planned",
      "scores": null
    }
  ]
}
`
	if string(raw) != want {
		t.Fatalf("manifest bytes diverge from v1 shape:\n%s", raw)
	}
}

// TestManifestNoHTMLEscaping pins the plan-solo parity fix: Python's
// json.dumps leaves <, >, & raw; Go's default marshal escapes them.
func TestManifestNoHTMLEscaping(t *testing.T) {
	s := newStore(t)
	m := Manifest{Slug: "sl", BaseBranch: "main", Branch: "b",
		Tasks: []Task{{ID: "01", Slug: "t", Title: `hello <name> & "friends"`, Deps: []string{}, Status: "planned"}}}
	if err := s.WriteManifest("p", "sl", m); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ReadArtifact("p", "sl", "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"hello <name> & \"friends\""`) {
		t.Fatalf("manifest escaping diverges from Python:\n%s", raw)
	}
}

func TestPathValidationRejectsTraversal(t *testing.T) {
	s := newStore(t)

	// Hostile project_id / slug.
	for _, bad := range []string{"../etc", "a/b", ".hidden", "", strings.Repeat("x", 200), "a..b/../c"} {
		if _, err := s.slugDir(bad, "ok"); err == nil {
			t.Errorf("project_id %q accepted", bad)
		}
		if _, err := s.slugDir("ok", bad); err == nil {
			t.Errorf("slug %q accepted", bad)
		}
	}

	// Hostile proposed-file paths (v1 _validate_rel_path matrix).
	if _, err := s.InitSlug("p", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "/abs/path", `\abs`, "a/../../escape", "..", "has\nnewline", strings.Repeat("y", 256), "C:\\win"} {
		_, err := s.WriteProposed("p", "sl", "01", "t", []ProposedFile{{Path: bad, Content: "x"}})
		if err == nil {
			t.Errorf("proposed path %q accepted", bad)
		}
	}

	// Valid nested path lands inside the tree.
	written, err := s.WriteProposed("p", "sl", "01", "t", []ProposedFile{{Path: "src/pkg/file.go", Content: "package pkg\n"}})
	if err != nil || len(written) != 1 {
		t.Fatalf("valid proposed write: %v %v", written, err)
	}
	if !strings.Contains(written[0], filepath.Join("build", "01-t", "proposed", "src", "pkg", "file.go")) {
		t.Fatalf("unexpected dest: %s", written[0])
	}

	// ReadArtifact refuses escape too.
	if _, err := s.ReadArtifact("p", "sl", "../../../etc/passwd"); err == nil {
		t.Fatal("ReadArtifact accepted traversal")
	}
}

func TestTaskAndBuildArtifacts(t *testing.T) {
	s := newStore(t)
	if _, err := s.InitSlug("p", "sl", "req", "main", "b", false); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteTaskArtifacts("p", "sl", "01", "auth", "ADR", "PLAN", "ACC", "CTX"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"adr.md": "ADR", "plan.md": "PLAN", "acceptance.md": "ACC", "context.md": "CTX"} {
		got, err := s.ReadArtifact("p", "sl", "tasks/01-auth/"+name)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
	if err := s.WriteBuildArtifact("p", "sl", "01", "auth", "verdict.md", []byte("PASS\n")); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadArtifact("p", "sl", "build/01-auth/verdict.md")
	if err != nil || string(got) != "PASS\n" {
		t.Fatalf("verdict = %q, %v", got, err)
	}
}

func TestJobPersistAndLoad(t *testing.T) {
	s := newStore(t)
	job := jobs.Job{
		ID:      jobs.Key{ProjectID: "p", Slug: "sl", Stage: "plan", TaskID: "t1"}.ID(),
		Key:     jobs.Key{ProjectID: "p", Slug: "sl", Stage: "plan", TaskID: "t1"},
		Attempt: 1, Status: jobs.StatusRunning, Progress: "task 1/2",
		SubmittedAt: time.Now().UTC(),
	}
	s.Persist(job)
	job.Status = jobs.StatusDone
	job.Result = json.RawMessage(`{"ok":true}`)
	s.Persist(job) // newer snapshot overwrites

	loaded, err := s.LoadJobs()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("LoadJobs = %v, %v", loaded, err)
	}
	if loaded[0].Status != jobs.StatusDone || string(loaded[0].Result) != `{"ok":true}` {
		t.Fatalf("loaded = %+v", loaded[0])
	}

	// Corrupt snapshot is skipped, not fatal.
	if err := os.WriteFile(filepath.Join(s.root, "jobs", "job_corrupt.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.LoadJobs()
	if err != nil || len(loaded) != 1 {
		t.Fatalf("LoadJobs with corrupt file = %d jobs, %v", len(loaded), err)
	}
}

// TestRunnerWithStorePersister wires the real runner to the real store —
// the M2 integration seam.
func TestRunnerWithStorePersister(t *testing.T) {
	s := newStore(t)
	r := jobs.NewRunner(2, s, nil)
	defer r.Close()

	key := jobs.Key{ProjectID: "p", Slug: "sl", Stage: "plan", TaskID: "t1"}
	job, _, err := r.Submit(key, "fp", jobs.Budgets{}, func(ctx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
		jc.Progress("working")
		return json.RawMessage(`{"n":1}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if j, ok := r.Get(job.ID); ok && j.Status.Terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err := s.LoadJobs()
	if err != nil || len(loaded) != 1 || loaded[0].Status != jobs.StatusDone {
		t.Fatalf("persisted terminal snapshot = %+v, %v", loaded, err)
	}
}

func TestAppendMetricConcurrent(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.AppendMetric(map[string]any{"phase": "build", "n": i, "schema_version": "build-2.0"})
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(s.root, "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 50 {
		t.Fatalf("metrics lines = %d, want 50", len(lines))
	}
	for _, ln := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("torn metrics line %q: %v", ln, err)
		}
	}
}
