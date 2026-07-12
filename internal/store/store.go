package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Store is the engine-owned artifact volume (ADR-005): canonical home for
// artifacts, job state, and central metrics, keyed (project_id, slug). Layout
// under root (the container's /data):
//
//	projects/<project_id>/<slug>/   request.md, manifest.json,
//	                                tasks/<id>-<taskslug>/{adr,plan,acceptance,context}.md,
//	                                build/<id>-<taskslug>/{proposed/**, review.md, verdict.md}
//	jobs/<job_id>.json              job snapshots (jobs.Persister)
//	metrics.jsonl                   central, append-only (schema build-2.0)
//
// The per-slug tree mirrors v1's in-repo local-fusion/<slug>/ exactly
// (orchestrator/fusion/artifacts.py) — the skill materializes it unchanged.
type Store struct {
	root string

	metricsMu sync.Mutex // serializes appends to metrics.jsonl
}

// New opens (creating if needed) a store rooted at dir.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty root dir")
	}
	for _, sub := range []string{"projects", "jobs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
	}
	return &Store{root: dir}, nil
}

// Root returns the volume root (diagnostics, ops tooling, tests).
func (s *Store) Root() string { return s.root }

// idRe constrains agent-supplied path components (project_id, slug, task ids).
// v1 trusted its own slugify; v2's server sits across a trust boundary and
// validates instead (ADR-004: this store is the only filesystem the engine has).
var idRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func validateID(kind, s string) error {
	if !idRe.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("invalid %s %q: must match %s and not contain '..'", kind, s, idRe)
	}
	return nil
}

// slugDir returns the per-slug root, validating every component.
func (s *Store) slugDir(projectID, slug string) (string, error) {
	if err := validateID("project_id", projectID); err != nil {
		return "", err
	}
	if err := validateID("slug", slug); err != nil {
		return "", err
	}
	return filepath.Join(s.root, "projects", projectID, slug), nil
}

// atomicWrite writes via tmp+rename so a crash never leaves a torn file.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// ValidateRelPath ports v1 artifacts.py::_validate_rel_path verbatim semantics:
// non-empty, single line, ≤255 chars, relative, no '..' component. Used for
// every agent- or model-supplied file path (FILE blocks, proposed files).
func ValidateRelPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("FILE block has an empty path")
	}
	if strings.ContainsAny(path, "\n\r") || len(path) > 255 {
		return "", fmt.Errorf("FILE block path is malformed (newline or too long): %.80q", path)
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return "", fmt.Errorf("FILE block path must be relative, got absolute: %q", path)
	}
	if filepath.IsAbs(path) || (len(path) > 1 && path[1] == ':') { // windows drive
		return "", fmt.Errorf("FILE block path must be relative: %q", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return "", fmt.Errorf("FILE block path must not contain '..': %q", path)
		}
	}
	return path, nil
}

// ─── manifest (schema unchanged from v1 — port contract) ────────────────────

// PyFloat marshals like Python's json.dumps floats: integral values keep a
// trailing ".0" ("9.0", not "9") — required for byte-parity of manifests,
// verdicts, and metrics against v1 (ADR-010).
type PyFloat float64

func (f PyFloat) MarshalJSON() ([]byte, error) {
	return []byte(PyFloatRepr(float64(f))), nil
}

// PyFloatRepr renders a float the way Python's repr/str does for the values
// this system produces (shortest round-trip, ".0" on integers).
func PyFloatRepr(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// ScoreSet is the judged-task score block, in v1's key order.
type ScoreSet struct {
	Req   PyFloat `json:"req"`
	Sec   PyFloat `json:"sec"`
	Maint PyFloat `json:"maint"`
	Avg   PyFloat `json:"avg"`
}

// Task is one manifest task entry; field set and order match v1 plan.py.
type Task struct {
	ID     string    `json:"id"`
	Slug   string    `json:"slug"`
	Title  string    `json:"title"`
	Deps   []string  `json:"deps"`
	Status string    `json:"status"`
	Scores *ScoreSet `json:"scores"`
}

// Manifest matches v1 artifacts.py/plan.py field-for-field, in order.
type Manifest struct {
	Slug       string `json:"slug"`
	BaseBranch string `json:"base_branch"`
	Branch     string `json:"branch"`
	Tasks      []Task `json:"tasks"`
}

// ErrExists reports an init on an existing slug without force (v1 semantics).
var ErrExists = errors.New("artifact folder already exists (pass force to overwrite)")

// InitSlug creates the slug tree with request.md and an empty manifest —
// v1 artifacts.py::init_slug.
func (s *Store) InitSlug(projectID, slug, requestText, baseBranch, branch string, force bool) (Manifest, error) {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return Manifest{}, err
	}
	if _, statErr := os.Stat(dir); statErr == nil && !force {
		return Manifest{}, fmt.Errorf("%w: %s/%s", ErrExists, projectID, slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	if err := atomicWrite(filepath.Join(dir, "request.md"), []byte(requestText)); err != nil {
		return Manifest{}, err
	}
	m := Manifest{Slug: slug, BaseBranch: baseBranch, Branch: branch, Tasks: []Task{}}
	return m, s.WriteManifest(projectID, slug, m)
}

// ReadManifest loads manifest.json for the slug.
func (s *Store) ReadManifest(projectID, slug string) (Manifest, error) {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest not found: %s/%s: %w", projectID, slug, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest corrupt: %s/%s: %w", projectID, slug, err)
	}
	return m, nil
}

// WriteManifest persists manifest.json (2-space indent + trailing newline,
// v1 formatting).
func (s *Store) WriteManifest(projectID, slug string, m Manifest) error {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "manifest.json"), append(data, '\n'))
}

// ─── task & build artifacts (v1 tree, verbatim names) ───────────────────────

func taskDirName(taskID, taskSlug string) string { return taskID + "-" + taskSlug }

// WriteTaskArtifacts writes the four planning artifacts for a task —
// v1 artifacts.py::write_task_artifacts.
func (s *Store) WriteTaskArtifacts(projectID, slug, taskID, taskSlug string, adr, plan, acceptance, context string) error {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return err
	}
	if err := validateID("task_id", taskID); err != nil {
		return err
	}
	if err := validateID("task_slug", taskSlug); err != nil {
		return err
	}
	base := filepath.Join(dir, "tasks", taskDirName(taskID, taskSlug))
	for name, content := range map[string]string{
		"adr.md": adr, "plan.md": plan, "acceptance.md": acceptance, "context.md": context,
	} {
		if err := atomicWrite(filepath.Join(base, name), []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

// WriteTaskArtifact writes a single named task artifact (e.g. an
// agent-supplied plan.md brief in M2, before the plan stage ports — briefs
// enter as data per the ADR-001 amendment).
func (s *Store) WriteTaskArtifact(projectID, slug, taskID, taskSlug, name string, content []byte) error {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return err
	}
	if err := validateID("task_id", taskID); err != nil {
		return err
	}
	if err := validateID("task_slug", taskSlug); err != nil {
		return err
	}
	if err := validateID("artifact name", name); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "tasks", taskDirName(taskID, taskSlug), name), content)
}

// WriteBuildArtifact writes one named build artifact (review.md, verdict.md).
func (s *Store) WriteBuildArtifact(projectID, slug, taskID, taskSlug, name string, content []byte) error {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return err
	}
	if err := validateID("task_id", taskID); err != nil {
		return err
	}
	if err := validateID("task_slug", taskSlug); err != nil {
		return err
	}
	if err := validateID("artifact name", name); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "build", taskDirName(taskID, taskSlug), name), content)
}

// ProposedFile is one model-emitted file (parsed from FILE blocks by the engine).
type ProposedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteProposed persists model-proposed files under build/<task>/proposed/ —
// v1 artifacts.py::write_proposed, with the same per-path validation.
func (s *Store) WriteProposed(projectID, slug, taskID, taskSlug string, files []ProposedFile) ([]string, error) {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return nil, err
	}
	if err := validateID("task_id", taskID); err != nil {
		return nil, err
	}
	if err := validateID("task_slug", taskSlug); err != nil {
		return nil, err
	}
	base := filepath.Join(dir, "build", taskDirName(taskID, taskSlug), "proposed")
	written := make([]string, 0, len(files))
	for _, f := range files {
		rel, err := ValidateRelPath(f.Path)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(base, filepath.FromSlash(rel))
		if err := atomicWrite(dest, []byte(f.Content)); err != nil {
			return nil, err
		}
		written = append(written, dest)
	}
	return written, nil
}

// ReadArtifact returns one artifact's bytes by slug-relative path (for tool
// results: every artifact is also returned as data — ADR-005). The rel path
// gets the same validation as writes.
func (s *Store) ReadArtifact(projectID, slug, rel string) ([]byte, error) {
	dir, err := s.slugDir(projectID, slug)
	if err != nil {
		return nil, err
	}
	clean, err := ValidateRelPath(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, filepath.FromSlash(clean)))
}

// ─── metrics (central, append-only) ──────────────────────────────────────────

// AppendMetric appends one JSON record to metrics.jsonl. v1 discipline kept:
// every live run logged; schema build-2.0 adds fields to v1's build-1.1,
// never changes existing ones (the engine owns record contents).
func (s *Store) AppendMetric(record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.root, "metrics.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
