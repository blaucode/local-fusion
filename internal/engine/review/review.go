// Package review ports v1 orchestrator/fusion/review.py: reviewer-panel
// resolution, sequential reviews, severity counting, review.md. Prompt
// wording comes exclusively from the frozen prompts/review.tmpl (ADR-008).
package review

import (
	"context"
	"fmt"
	"strings"

	prompts "local-fusion"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// Finding is one reviewer's raw response (v1 returns {model_key, text}).
type Finding struct {
	ModelKey string `json:"model_key"`
	Text     string `json:"text"`
}

// Result mirrors v1 review_task's return dict.
type Result struct {
	Findings  []Finding `json:"findings"`
	Critical  int       `json:"critical"`
	Important int       `json:"important"`
	Minor     int       `json:"minor"`
}

// Deps are the review stage's collaborators.
type Deps struct {
	Store  *store.Store
	Cfg    *providers.Config
	Caller providers.Caller
	Log    func(string)
}

func buildPrompts(brief, implementation string) ([]providers.Message, error) {
	sysTpl, err := prompts.Block("review", 1)
	if err != nil {
		return nil, err
	}
	system, err := sysTpl.Render(nil)
	if err != nil {
		return nil, err
	}
	userTpl, err := prompts.Block("review", 2)
	if err != nil {
		return nil, err
	}
	user, err := userTpl.Render(map[string]string{
		"brief":          brief,
		"implementation": implementation,
	})
	if err != nil {
		return nil, err
	}
	return []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, nil
}

// CountSeverities ports review.py::_count_severities.
func CountSeverities(responses []string) (critical, important, minor int) {
	for _, r := range responses {
		for _, line := range strings.Split(r, "\n") {
			l := strings.ToLower(strings.TrimSpace(line))
			if !strings.HasPrefix(l, "severity:") {
				continue
			}
			val := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
			switch {
			case strings.Contains(val, "critical"):
				critical++
			case strings.Contains(val, "important"):
				important++
			case strings.Contains(val, "minor"):
				minor++
			}
		}
	}
	return
}

// Task ports review.py::review_task. Reviewers run sequentially (v1 request
// order, for replay parity) with call_model defaults (max_tokens 8192,
// timeout 190s), degrading per-reviewer on failure.
func Task(ctx context.Context, d Deps, projectID, slug, taskID, taskSlug, changedFiles, pipeline string) (Result, error) {
	log := d.Log
	if log == nil {
		log = func(string) {}
	}
	if pipeline == "" {
		pipeline = "default"
	}

	briefBytes, err := d.Store.ReadArtifact(projectID, slug, "tasks/"+taskID+"-"+taskSlug+"/plan.md")
	if err != nil {
		return Result{}, fmt.Errorf("task brief missing for %s-%s: run planning first (or pass brief)", taskID, taskSlug)
	}
	brief := string(briefBytes)

	if strings.TrimSpace(changedFiles) == "" {
		return Result{}, fmt.Errorf("no implementation content provided to review")
	}

	if _, ok := d.Cfg.Pipelines[pipeline]; !ok {
		return Result{}, fmt.Errorf("pipeline '%s' not found in config", pipeline)
	}

	// role="reviewer" so this reads reviewer_panel (incl. its providers
	// restriction); falls back to tl scores when reviewer scores are absent.
	reviewers, err := d.Cfg.ResolveRoleModels(pipeline, "reviewer", 0, log)
	if err != nil {
		return Result{}, err
	}
	if len(reviewers) == 0 {
		return Result{}, fmt.Errorf("no reviewer models available")
	}

	log(fmt.Sprintf("[review] task %s: %d reviewers...", taskID, len(reviewers)))

	messages, err := buildPrompts(brief, changedFiles)
	if err != nil {
		return Result{}, err
	}

	type resp struct {
		key  string
		text string
		ok   bool
	}
	var responses []resp
	for i, rv := range reviewers {
		label := fmt.Sprintf("reviewer %d/%d (%s)", i+1, len(reviewers), rv.Key)
		out, ok := d.Caller.CallModel(ctx, providers.CallRequest{
			ModelKey: rv.Key, ModelID: rv.Model.ID, BaseURL: rv.Provider.BaseURL,
			EnvKey: rv.Provider.EnvKey, Messages: messages,
			MaxTokens: 8192, Label: label, // v1 call_model defaults (timeout 190s in client)
		})
		responses = append(responses, resp{key: rv.Key, text: out, ok: ok})
	}

	var valid []resp
	for _, r := range responses {
		if r.ok {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return Result{}, fmt.Errorf("all reviewer calls failed")
	}

	lines := []string{"=== CODE REVIEW REPORT ===", fmt.Sprintf("Reviewers: %d models", len(valid)), ""}
	for i, r := range valid {
		lines = append(lines, fmt.Sprintf("--- Reviewer %d (%s) ---", i+1, r.key), r.text, "")
	}
	texts := make([]string, len(valid))
	for i, r := range valid {
		texts[i] = r.text
	}
	critical, important, minor := CountSeverities(texts)
	lines = append(lines, "=== SUMMARY ===", fmt.Sprintf("Critical: %d  Important: %d  Minor: %d", critical, important, minor))

	report := strings.Join(lines, "\n")
	if err := d.Store.WriteBuildArtifact(projectID, slug, taskID, taskSlug, "review.md", []byte(report+"\n")); err != nil {
		return Result{}, err
	}

	if err := markReviewed(d.Store, projectID, slug, taskID); err != nil {
		log(fmt.Sprintf("Warning: failed to update manifest: %v", err))
	}

	findings := make([]Finding, len(valid))
	for i, r := range valid {
		findings[i] = Finding{ModelKey: r.key, Text: r.text}
	}
	return Result{Findings: findings, Critical: critical, Important: important, Minor: minor}, nil
}

// markReviewed ports review.py::_mark_reviewed.
func markReviewed(s *store.Store, projectID, slug, taskID string) error {
	m, err := s.ReadManifest(projectID, slug)
	if err != nil {
		return err
	}
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			m.Tasks[i].Status = "reviewed"
		}
	}
	return s.WriteManifest(projectID, slug, m)
}
