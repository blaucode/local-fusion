// Package plan ports v1 orchestrator/fusion/plan.py — M3 lands it in stages:
// plan-solo first (decompose + haft deliberation + section split; this file),
// plan-full (TL panel + synthesizer) behind its own parity gate later.
// Prompt wording comes exclusively from frozen prompts/plan.tmpl blocks
// (ADR-008): 1-2 decompose, 3-4 h-frame, 5-6 h-explore, 7-8 h-compare.
//
// v2 differences, all decided upstream: no gitops (the agent owns git and
// attests — ADR-004), intent attestation required (ADR-011), runs as an async
// job (ADR-003) with engine budgets (ADR-007).
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	prompts "local-fusion"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// Slugify ports artifacts.py::slugify (ASCII semantics; model-emitted slugs
// are kebab ASCII in practice).
func Slugify(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = regexp.MustCompile(`[\s_]+`).ReplaceAllString(text, "-")
	text = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`-+`).ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	if text == "" {
		return "untitled"
	}
	return text
}

// DecomposedTask is one entry from the decompose call (v1 dict shape).
type DecomposedTask struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Deps    []string `json:"deps"`
}

var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// extractJSONArray ports plan.py::_extract_json_array.
func extractJSONArray(text string) []any {
	if text == "" {
		return nil
	}
	if m := fenceRe.FindStringSubmatch(text); m != nil {
		text = m[1]
	}
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end < start {
		return nil
	}
	var parsed []any
	if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err != nil {
		return nil
	}
	return parsed
}

// fallbackTasks ports plan.py::_fallback_tasks.
func fallbackTasks(requestText string) []DecomposedTask {
	trimmed := strings.TrimSpace(requestText)
	firstLine := "task"
	if trimmed != "" {
		firstLine = strings.SplitN(trimmed, "\n", 2)[0]
	}
	title := firstLine
	if len(title) > 80 {
		title = title[:80]
	}
	if title == "" {
		title = "Task"
	}
	slugSrc := firstLine
	if len(slugSrc) > 60 {
		slugSrc = slugSrc[:60]
	}
	return []DecomposedTask{{
		Slug: Slugify(slugSrc), Title: title, Summary: trimmed, Deps: []string{},
	}}
}

func contextStr(context string) string {
	if context == "" {
		return "(no code context provided)"
	}
	return context
}

// Deps are the plan stage's collaborators. Progress/log flow through the job
// context at the call site; the engine takes plain funcs.
type Deps struct {
	Store  *store.Store
	Cfg    *providers.Config
	Caller providers.Caller
	Log    func(string)
}

func renderBlock(n int, vars map[string]string) (string, error) {
	tpl, err := prompts.Block("plan", n)
	if err != nil {
		return "", err
	}
	return tpl.Render(vars)
}

// Decompose ports plan.py::decompose: one TL call → ordered task list, with
// the single-task fallback on any parse failure. Returns tasks + optional
// truncation note.
func Decompose(ctx context.Context, d Deps, requestText, codeContext, pipeline string) ([]DecomposedTask, string, error) {
	tl, err := d.Cfg.ResolveRoleModels(pipeline, "tl", 1, d.Log)
	if err != nil || len(tl) == 0 {
		return fallbackTasks(requestText), "", nil
	}
	system, err := renderBlock(1, nil)
	if err != nil {
		return nil, "", err
	}
	user, err := renderBlock(2, map[string]string{
		"request_text": requestText, "context_str": contextStr(codeContext),
	})
	if err != nil {
		return nil, "", err
	}
	out, ok := d.Caller.CallModel(ctx, providers.CallRequest{
		ModelKey: tl[0].Key, ModelID: tl[0].Model.ID, BaseURL: tl[0].Provider.BaseURL,
		EnvKey: tl[0].Provider.EnvKey, MaxTokens: 8192, Label: "decompose",
		Messages: []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
	})
	if !ok {
		d.Log("[decompose] could not parse task list; falling back to a single task.")
		return fallbackTasks(requestText), "", nil
	}
	parsed := extractJSONArray(out)
	if parsed == nil {
		d.Log("[decompose] could not parse task list; falling back to a single task.")
		return fallbackTasks(requestText), "", nil
	}

	var tasks []DecomposedTask
	for _, item := range parsed {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title := strings.TrimSpace(firstNonEmpty(str(m["title"]), str(m["summary"]), "task"))
		slugSrc := firstNonEmpty(str(m["slug"]), title)
		if len(slugSrc) > 60 {
			slugSrc = slugSrc[:60]
		}
		summary := strings.TrimSpace(firstNonEmpty(str(m["summary"]), title))
		var deps []string
		if dl, ok := m["deps"].([]any); ok {
			for _, dep := range dl {
				deps = append(deps, Slugify(str(dep)))
			}
		}
		if deps == nil {
			deps = []string{}
		}
		if len(title) > 120 {
			title = title[:120]
		}
		if title == "" {
			title = Slugify(slugSrc)
		}
		tasks = append(tasks, DecomposedTask{Slug: Slugify(slugSrc), Title: title, Summary: summary, Deps: deps})
	}
	if len(tasks) == 0 {
		return fallbackTasks(requestText), "", nil
	}
	note := ""
	if len(tasks) > 8 {
		note = fmt.Sprintf("Model returned %d tasks; kept the first 8.", len(tasks))
		tasks = tasks[:8]
	}
	return tasks, note, nil
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RunHaft ports plan.py::run_haft: frame → explore → compare, sequential on
// the primary TL model; any failed step fails the task (fatal, ADR-007 style).
func RunHaft(ctx context.Context, d Deps, taskText, codeContext string, tl providers.Resolved) (string, error) {
	call := func(sysBlock, userBlock int, vars map[string]string, label string) (string, error) {
		system, err := renderBlock(sysBlock, nil)
		if err != nil {
			return "", err
		}
		user, err := renderBlock(userBlock, vars)
		if err != nil {
			return "", err
		}
		out, ok := d.Caller.CallModel(ctx, providers.CallRequest{
			ModelKey: tl.Key, ModelID: tl.Model.ID, BaseURL: tl.Provider.BaseURL,
			EnvKey: tl.Provider.EnvKey, MaxTokens: 8192, Label: label,
			Messages: []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		})
		if !ok {
			return "", fmt.Errorf("%s stage failed", label)
		}
		return out, nil
	}

	frame, err := call(3, 4, map[string]string{
		"task_text": taskText, "context_str": contextStr(codeContext),
	}, "h-frame")
	if err != nil {
		return "", err
	}
	explore, err := call(5, 6, map[string]string{"frame": frame}, "h-explore")
	if err != nil {
		return "", err
	}
	compare, err := call(7, 8, map[string]string{"frame": frame, "explore": explore}, "h-compare")
	if err != nil {
		return "", err
	}
	return compare, nil
}

var sectionRe = regexp.MustCompile(`(?im)^##\s+(ADR|PLAN|ACCEPTANCE)\s*$`)

// SplitSections ports plan.py::_split_sections.
func SplitSections(text string) (adr, plan, acceptance string, found bool) {
	if text == "" {
		return "", "", "", false
	}
	sections := map[string]string{}
	matches := sectionRe.FindAllStringSubmatchIndex(text, -1)
	for i, m := range matches {
		name := strings.ToUpper(text[m[2]:m[3]])
		start := m[1]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		sections[name] = strings.TrimSpace(text[start:end])
	}
	return sections["ADR"], sections["PLAN"], sections["ACCEPTANCE"], len(sections) > 0
}

// ScopeMD ports plan.py::_scope_md.
func ScopeMD(slug string, tasks []DecomposedTask, truncationNote string) string {
	lines := []string{fmt.Sprintf("# Scope: %s", slug), ""}
	if truncationNote != "" {
		lines = append(lines, fmt.Sprintf("> Note: %s", truncationNote), "")
	}
	lines = append(lines, "## Tasks", "")
	for i, task := range tasks {
		deps := "none"
		if len(task.Deps) > 0 {
			deps = strings.Join(task.Deps, ", ")
		}
		lines = append(lines,
			fmt.Sprintf("%d. **%s** (`%s`)", i+1, task.Title, task.Slug),
			fmt.Sprintf("   - %s", task.Summary),
			fmt.Sprintf("   - deps: %s", deps),
			"")
	}
	return strings.Join(lines, "\n")
}

// Progress is the stage's narration hook (wired to JobContext.Progress).
type Progress func(string)

// SoloResult is what the plan-solo job returns (also persisted as manifest).
type SoloResult struct {
	Manifest store.Manifest `json:"manifest"`
}

// Solo ports plan_feature's no_fusion path, minus gitops (agent owns git,
// ADR-004): init slug, decompose, scope.md, per task haft → split sections →
// task artifacts, manifest. baseBranch/branch come from the agent's git_state
// attestation; requestMD is request text + attestation audit block (ADR-011).
func Solo(ctx context.Context, d Deps, progress Progress,
	projectID, slug, requestText, requestMD, codeContext, pipeline, baseBranch, branch string,
	intent store.Intent, force bool) (SoloResult, error) {

	if progress == nil {
		progress = func(string) {}
	}
	if pipeline == "" {
		pipeline = "default"
	}

	if _, err := d.Store.InitSlug(projectID, slug, requestMD, baseBranch, branch, force); err != nil {
		return SoloResult{}, err
	}

	progress("decomposing request")
	d.Log("[plan] decomposing request...")
	tasks, note, err := Decompose(ctx, d, requestText, codeContext, pipeline)
	if err != nil {
		return SoloResult{}, err
	}
	if note != "" {
		d.Log("[plan] " + note)
	}
	d.Log(fmt.Sprintf("[plan] %d task(s)", len(tasks)))

	if err := d.Store.WriteSlugArtifact(projectID, slug, "scope.md", []byte(ScopeMD(slug, tasks, note))); err != nil {
		return SoloResult{}, err
	}

	tlModels, err := d.Cfg.ResolveRoleModels(pipeline, "tl", 0, d.Log)
	if err != nil || len(tlModels) == 0 {
		return SoloResult{}, fmt.Errorf("no TL models available in pipeline '%s'", pipeline)
	}
	primary := tlModels[0]

	var manifestTasks []store.Task
	for i, task := range tasks {
		taskID := fmt.Sprintf("%02d", i+1)
		taskText := task.Title + "\n\n" + task.Summary
		progress(fmt.Sprintf("task %d/%d (%s): haft deliberation", i+1, len(tasks), task.Slug))
		d.Log(fmt.Sprintf("[plan] task %s (%s): haft deliberation...", taskID, task.Slug))

		synthesis, err := RunHaft(ctx, d, taskText, codeContext, primary)
		if err != nil {
			return SoloResult{}, fmt.Errorf("task %s: %w", taskID, err)
		}

		adr, planText, acceptance, _ := SplitSections(synthesis)
		if planText == "" {
			planText = synthesis
		}
		if adr == "" {
			adr = "(no fusion; see plan)"
		}
		if acceptance == "" {
			acceptance = "(no fusion; see plan)"
		}

		if err := d.Store.WriteTaskArtifacts(projectID, slug, taskID, task.Slug, adr, planText, acceptance, codeContext); err != nil {
			return SoloResult{}, err
		}
		manifestTasks = append(manifestTasks, store.Task{
			ID: taskID, Slug: task.Slug, Title: task.Title, Deps: task.Deps,
			Status: "planned", Scores: nil,
		})
	}

	manifest := store.Manifest{
		Slug: slug, BaseBranch: baseBranch, Branch: branch, Tasks: manifestTasks,
		Intent: &intent,
	}
	if err := d.Store.WriteManifest(projectID, slug, manifest); err != nil {
		return SoloResult{}, err
	}
	return SoloResult{Manifest: manifest}, nil
}
