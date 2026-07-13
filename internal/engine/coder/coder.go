// Package coder ports v1 orchestrator/fusion/coder_fusion.py: the solo coder
// path and the two-coder fusion path (evaluator picks base + grafts, lead
// merges), with every degradation rung intact (ADR-009: a port, never an
// improvement). Prompt wording and the FILE-block contract come exclusively
// from frozen prompts/{coder_fusion,artifacts}.tmpl (ADR-008).
package coder

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	prompts "local-fusion"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

var (
	baseRe = regexp.MustCompile(`(?im)^\s*BASE:\s*(A|B)\b`)

	fileBlockOnce sync.Once
	fileBlockRe   *regexp.Regexp
	fileBlockErr  error
)

// fileBlockRegexp compiles v1's _FILE_BLOCK_RE from the frozen extraction
// (artifacts.tmpl block 2, an r-string) — Python re.DOTALL becomes (?s).
func fileBlockRegexp() (*regexp.Regexp, error) {
	fileBlockOnce.Do(func() {
		tpl, err := prompts.Block("artifacts", 2)
		if err != nil {
			fileBlockErr = err
			return
		}
		src, err := tpl.Render(nil)
		if err != nil {
			fileBlockErr = err
			return
		}
		fileBlockRe, fileBlockErr = regexp.Compile("(?s)" + src)
	})
	return fileBlockRe, fileBlockErr
}

// emitInstructions renders v1's EMIT_INSTRUCTIONS (artifacts.tmpl block 1).
func emitInstructions() (string, error) {
	tpl, err := prompts.Block("artifacts", 1)
	if err != nil {
		return "", err
	}
	return tpl.Render(nil)
}

// ParseFileBlocks ports artifacts.py::parse_file_blocks, including per-path
// validation (any bad path fails the whole parse, as v1's exception does).
func ParseFileBlocks(text string) ([]store.ProposedFile, error) {
	if text == "" {
		return nil, nil
	}
	re, err := fileBlockRegexp()
	if err != nil {
		return nil, err
	}
	var files []store.ProposedFile
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		path, err := store.ValidateRelPath(m[1])
		if err != nil {
			return nil, err
		}
		files = append(files, store.ProposedFile{Path: path, Content: m[2]})
	}
	return files, nil
}

// ParseBase ports coder_fusion.py::parse_base: default A when unparseable.
func ParseBase(text string, log func(string)) string {
	if text != "" {
		if m := baseRe.FindStringSubmatch(text); m != nil {
			return strings.ToUpper(m[1])
		}
	}
	log("[coder-fusion] could not parse BASE from evaluator output; defaulting to A.")
	return "A"
}

// Deps are the coder stage's collaborators.
type Deps struct {
	Store  *store.Store
	Cfg    *providers.Config
	Caller providers.Caller
	Log    func(string)
	// User and ServerVersion feed the metrics additions (cf-2.0).
	User          string
	ServerVersion string
}

// runCoder ports coder_fusion.py::run_coder: 16384 output tokens (32K-total
// models reject more), 420s + one retry.
func runCoder(ctx context.Context, d Deps, taskPlan, acceptance, codeContext string, m providers.Resolved, label string) (string, bool, error) {
	tpl, err := prompts.Block("coder_fusion", 1)
	if err != nil {
		return "", false, err
	}
	head, err := tpl.Render(map[string]string{
		"task_plan": taskPlan, "acceptance": acceptance, "context": codeContext,
	})
	if err != nil {
		return "", false, err
	}
	emit, err := emitInstructions()
	if err != nil {
		return "", false, err
	}
	req := providers.CallRequest{
		ModelKey: m.Key, ModelID: m.Model.ID, BaseURL: m.Provider.BaseURL,
		EnvKey: m.Provider.EnvKey, MaxTokens: 16384, Timeout: 420 * time.Second, Label: label,
		Messages: []providers.Message{{Role: "user", Content: head + emit}},
	}
	out, ok := d.Caller.CallModel(ctx, req)
	if !ok {
		retry := req
		retry.Label = label + " (retry)"
		out, ok = d.Caller.CallModel(ctx, retry)
	}
	return out, ok, nil
}

// runEvaluator ports coder_fusion.py::run_evaluator (call_model defaults).
func runEvaluator(ctx context.Context, d Deps, taskPlan, acceptance, solA, solB string, m providers.Resolved) (string, bool, error) {
	tpl, err := prompts.Block("coder_fusion", 2)
	if err != nil {
		return "", false, err
	}
	prompt, err := tpl.Render(map[string]string{
		"task_plan": taskPlan, "acceptance": acceptance, "sol_a": solA, "sol_b": solB,
	})
	if err != nil {
		return "", false, err
	}
	out, ok := d.Caller.CallModel(ctx, providers.CallRequest{
		ModelKey: m.Key, ModelID: m.Model.ID, BaseURL: m.Provider.BaseURL,
		EnvKey: m.Provider.EnvKey, MaxTokens: 8192, Label: "evaluator",
		Messages: []providers.Message{{Role: "user", Content: prompt}},
	})
	return out, ok, nil
}

// runLead ports coder_fusion.py::run_lead: base + named grafts only (never
// the full loser solution — context windows), 32768 tokens, 420s + one retry.
func runLead(ctx context.Context, d Deps, taskPlan, baseSolution, graftsText string, m providers.Resolved) (string, bool, error) {
	tpl, err := prompts.Block("coder_fusion", 3)
	if err != nil {
		return "", false, err
	}
	head, err := tpl.Render(map[string]string{
		"task_plan": taskPlan, "base_solution": baseSolution, "grafts_text": graftsText,
	})
	if err != nil {
		return "", false, err
	}
	emit, err := emitInstructions()
	if err != nil {
		return "", false, err
	}
	req := providers.CallRequest{
		ModelKey: m.Key, ModelID: m.Model.ID, BaseURL: m.Provider.BaseURL,
		EnvKey: m.Provider.EnvKey, MaxTokens: 32768, Timeout: 420 * time.Second, Label: "lead",
		Messages: []providers.Message{{Role: "user", Content: head + emit}},
	}
	out, ok := d.Caller.CallModel(ctx, req)
	if !ok {
		retry := req
		retry.Label = "lead (retry)"
		out, ok = d.Caller.CallModel(ctx, retry)
	}
	return out, ok, nil
}

// Result mirrors v1 coder_fusion_task's return dict.
type Result struct {
	Files      []store.ProposedFile `json:"files"`
	BaseChosen string               `json:"base_chosen"`
	Notes      string               `json:"notes"`
}

// cfMetrics is v1's cf-1.0 record plus the v2 additions (cf-2.0 — fields are
// added, never changed, same convention as build-2.0).
type cfMetrics struct {
	Phase      string `json:"phase"`
	Slug       string `json:"slug"`
	TaskID     string `json:"task_id"`
	BaseChosen string `json:"base_chosen"`
	Solo       bool   `json:"solo"`
	Date       string `json:"date"`
	SchemaVer  string `json:"schema_version"`
	User       string `json:"user"`
	Repo       string `json:"repo"`
	ServerVer  string `json:"server_version"`
}

// Task ports coder_fusion.py::coder_fusion_task — solo and fusion paths with
// v1's full degradation ladder: coder failure → survivor; evaluator failure →
// base A, no grafts; lead failure → base without grafts; only "no usable FILE
// blocks anywhere" aborts.
func Task(ctx context.Context, d Deps, projectID, slug, taskID, taskSlug, codeContext, pipeline string, solo bool) (Result, error) {
	log := d.Log
	if log == nil {
		log = func(string) {}
	}
	if pipeline == "" {
		pipeline = "default"
	}

	taskPlan, acceptance, err := readTaskBrief(d.Store, projectID, slug, taskID, taskSlug)
	if err != nil {
		return Result{}, err
	}

	names, err := coderFusionNames(d.Cfg, pipeline)
	if err != nil {
		return Result{}, err
	}

	coderA, err := d.Cfg.ResolveNamed(names.CoderA)
	if err != nil {
		return Result{}, err
	}

	finish := func(files []store.ProposedFile, baseChosen, notes string, soloRun bool) (Result, error) {
		if _, err := d.Store.WriteProposed(projectID, slug, taskID, taskSlug, files); err != nil {
			return Result{}, err
		}
		if err := markImplemented(d.Store, projectID, slug, taskID); err != nil {
			log(fmt.Sprintf("[coder-fusion] warning: could not update manifest: %v", err))
		}
		entry := cfMetrics{
			Phase: "coder-fusion", Slug: slug, TaskID: taskID, BaseChosen: baseChosen,
			Solo: soloRun, Date: time.Now().UTC().Format("2006-01-02"), SchemaVer: "cf-2.0",
			User: d.User, Repo: projectID, ServerVer: d.ServerVersion,
		}
		if err := d.Store.AppendMetric(entry); err != nil {
			log(fmt.Sprintf("[coder-fusion] warning: could not write metrics: %v", err))
		}
		return Result{Files: files, BaseChosen: baseChosen, Notes: notes}, nil
	}

	if solo {
		log(fmt.Sprintf("[coder-fusion] task %s: solo coder (%s)...", taskID, names.CoderA))
		out, ok, err := runCoder(ctx, d, taskPlan, acceptance, codeContext, coderA, "coder-a")
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, fmt.Errorf("solo coder failed to produce output")
		}
		files, err := ParseFileBlocks(out)
		if err != nil {
			return Result{}, err
		}
		if len(files) == 0 {
			return Result{}, fmt.Errorf("solo coder produced no FILE blocks")
		}
		return finish(files, "solo", "solo coder", true)
	}

	coderB, err := d.Cfg.ResolveNamed(names.CoderB)
	if err != nil {
		return Result{}, err
	}

	log(fmt.Sprintf("[coder-fusion] task %s: running coder-a (%s) + coder-b (%s) in parallel...", taskID, names.CoderA, names.CoderB))
	var (
		wg         sync.WaitGroup
		solA, solB string
		okA, okB   bool
		errA, errB error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		solA, okA, errA = runCoder(ctx, d, taskPlan, acceptance, codeContext, coderA, "coder-a")
	}()
	go func() {
		defer wg.Done()
		solB, okB, errB = runCoder(ctx, d, taskPlan, acceptance, codeContext, coderB, "coder-b")
	}()
	wg.Wait()
	if errA != nil {
		return Result{}, errA
	}
	if errB != nil {
		return Result{}, errB
	}

	if !okA && !okB {
		return Result{}, fmt.Errorf("both coders failed to produce output")
	}
	if !okA || !okB {
		survivorLetter, survivorText, failed := "A", solA, "coder-b"
		if !okA {
			survivorLetter, survivorText, failed = "B", solB, "coder-a"
		}
		log(fmt.Sprintf("[coder-fusion] warning: %s failed; using surviving solution %s as final (degraded).", failed, survivorLetter))
		files, err := ParseFileBlocks(survivorText)
		if err != nil {
			return Result{}, err
		}
		if len(files) == 0 {
			return Result{}, fmt.Errorf("surviving coder %s produced no FILE blocks", survivorLetter)
		}
		return finish(files, survivorLetter,
			fmt.Sprintf("degraded: %s failed, used %s directly (no evaluator/lead)", failed, survivorLetter), false)
	}

	evaluator, err := d.Cfg.ResolveNamed(names.Evaluator)
	if err != nil {
		return Result{}, err
	}
	lead, err := d.Cfg.ResolveNamed(names.Lead)
	if err != nil {
		return Result{}, err
	}

	log(fmt.Sprintf("[coder-fusion] task %s: evaluator (%s) picking base + grafts...", taskID, names.Evaluator))
	evalOut, ok, err := runEvaluator(ctx, d, taskPlan, acceptance, solA, solB, evaluator)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		log("[coder-fusion] warning: evaluator failed; defaulting BASE to A with no grafts.")
		evalOut = ""
	}

	baseLetter := ParseBase(evalOut, log)
	baseSolution := solA
	if baseLetter == "B" {
		baseSolution = solB
	}

	log(fmt.Sprintf("[coder-fusion] task %s: lead (%s) grafting base %s -> final...", taskID, names.Lead, baseLetter))
	leadOut, ok, err := runLead(ctx, d, taskPlan, baseSolution, evalOut, lead)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		leadOut = ""
	}

	files, err := ParseFileBlocks(leadOut)
	if err != nil {
		return Result{}, err
	}
	degradedNote := ""
	if len(files) == 0 {
		// Lead failed or emitted nothing (e.g. context overflow) — degrade to
		// the evaluator's chosen base, which is complete on its own. Never abort here.
		log(fmt.Sprintf("[coder-fusion] warning: lead produced no usable output; falling back to base %s (grafts skipped).", baseLetter))
		files, err = ParseFileBlocks(baseSolution)
		if err != nil {
			return Result{}, err
		}
		if len(files) == 0 {
			return Result{}, fmt.Errorf("lead failed and base %s has no FILE blocks either", baseLetter)
		}
		degradedNote = " (lead failed; used base without grafts)"
	}

	graftCount := 0
	for _, line := range strings.Split(evalOut, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-") {
			graftCount++
		}
	}
	notes := fmt.Sprintf("base %s + %d graft(s) via lead", baseLetter, graftCount)
	if degradedNote != "" {
		notes = fmt.Sprintf("base %s%s", baseLetter, degradedNote)
	}
	return finish(files, baseLetter, notes, false)
}

// fusionNames holds the coder_fusion pipeline block's model keys.
type fusionNames struct {
	CoderA, CoderB, Evaluator, Lead string
}

// coderFusionNames reads the coder_fusion block (flat model-name fields —
// a different shape from the <role>_panel maps, so it bypasses Panel).
func coderFusionNames(cfg *providers.Config, pipeline string) (fusionNames, error) {
	names, err := cfg.CoderFusionBlock(pipeline)
	if err != nil {
		return fusionNames{}, err
	}
	return fusionNames{
		CoderA: names["coder_a"], CoderB: names["coder_b"],
		Evaluator: names["evaluator"], Lead: names["lead"],
	}, nil
}

func readTaskBrief(s *store.Store, projectID, slug, taskID, taskSlug string) (string, string, error) {
	planMD, err := s.ReadArtifact(projectID, slug, "tasks/"+taskID+"-"+taskSlug+"/plan.md")
	if err != nil {
		return "", "", fmt.Errorf("task brief missing for %s-%s: run planning first", taskID, taskSlug)
	}
	acc, err := s.ReadArtifact(projectID, slug, "tasks/"+taskID+"-"+taskSlug+"/acceptance.md")
	if err != nil {
		return "", "", fmt.Errorf("task brief missing for %s-%s: run planning first", taskID, taskSlug)
	}
	return string(planMD), string(acc), nil
}

func markImplemented(s *store.Store, projectID, slug, taskID string) error {
	m, err := s.ReadManifest(projectID, slug)
	if err != nil {
		return err
	}
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			m.Tasks[i].Status = "implemented"
		}
	}
	return s.WriteManifest(projectID, slug, m)
}
