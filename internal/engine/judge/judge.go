// Package judge ports v1 orchestrator/fusion/judge.py line-for-line: the
// deterministic test gate (ADR-006), dual-judge scoring, verdict.md, the
// metrics record, and the manifest update. Prompt wording comes exclusively
// from the frozen prompts/judge.tmpl blocks (ADR-008).
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	prompts "local-fusion"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

// TestReport is v1's normalized report: {command, exit_code, summary}.
type TestReport struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Summary  string `json:"summary"`
}

// NormalizeTestReport ports judge.py::normalize_test_report: accept a dict or
// JSON string; nil/empty → (nil, nil); malformed → error ("a gate that
// silently ignores bad evidence is worse than no gate").
func NormalizeTestReport(input any) (*TestReport, error) {
	if input == nil || input == "" {
		return nil, nil
	}
	if s, ok := input.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return nil, fmt.Errorf("test_report is not valid JSON: %v", err)
		}
		input = decoded
	}
	m, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`test_report must be an object like {"command": "bin/phpunit", "exit_code": 0, "summary": "78 passed"}`)
	}
	if len(m) == 0 {
		return nil, nil
	}
	rawExit, ok := m["exit_code"]
	if !ok {
		return nil, fmt.Errorf(`test_report missing required field "exit_code"`)
	}
	exitCode, err := toInt(rawExit)
	if err != nil {
		return nil, fmt.Errorf("test_report exit_code must be an integer, got: %v", rawExit)
	}
	return &TestReport{
		Command:  toStr(m["command"]),
		ExitCode: exitCode,
		Summary:  truncateRunes(toStr(m["summary"]), 2000),
	}, nil
}

// toInt ports Python int(x) for the JSON-borne types.
func toInt(v any) (int, error) {
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case json.Number:
		f, err := x.Float64()
		return int(f), err
	case string:
		return strconv.Atoi(strings.TrimSpace(x))
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// JudgeResult is one judge's parsed scores (v1 parse_scores dict + model_key).
type JudgeResult struct {
	Req, Sec, Maint, Avg float64
	Verdict              string
	Notes                string
	ModelKey             string
}

// Aggregate is the dual-judge aggregate (v1 aggregate + test/coverage gate fields).
type Aggregate struct {
	Req, Sec, Maint, Avg float64
	Verdict              string
	Judges               []JudgeResult
	Tests                *TestReport
	GateReason           string
	// Coverage is the acceptance-coverage result (ADR-014); nil when the task
	// has no parseable acceptance criteria (gate inert, parity-safe).
	Coverage *CoverageResult
}

// CoverageResult records acceptance-criteria coverage (ADR-014): the ordered
// criteria parsed from acceptance.md and which of them the caller's
// acceptance_coverage attestation left uncovered.
type CoverageResult struct {
	Criteria  []string `json:"criteria"`
	Uncovered []string `json:"uncovered"`
	Attested  bool     `json:"attested"`
}

// acceptanceItemRe matches one acceptance criterion: a bullet (-, *), an
// optional checkbox, or a numbered item — "a checklist, one item per line"
// (the synthesizer's ACCEPTANCE section). Prose lines are ignored.
var acceptanceItemRe = regexp.MustCompile(`(?m)^[ \t]*(?:[-*][ \t]+(?:\[[ xX]\][ \t]+)?|\d+[.)][ \t]+)(\S.*?)[ \t]*$`)

// ParseAcceptanceCriteria extracts the ordered criteria from acceptance.md.
func ParseAcceptanceCriteria(md string) []string {
	var out []string
	for _, m := range acceptanceItemRe.FindAllStringSubmatch(md, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// ApplyAcceptanceGate ports ADR-014: PASS additionally requires every
// acceptance criterion to be covered. coverage[i] (non-empty) attests
// criteria[i]. Inert when there are no criteria (parity-safe). Applied after
// the test gate; a pre-existing FAIL reason (red tests) is kept.
func ApplyAcceptanceGate(agg Aggregate, criteria, coverage []string) Aggregate {
	if len(criteria) == 0 {
		return agg
	}
	cov := &CoverageResult{Criteria: criteria, Attested: len(coverage) > 0}
	for i, c := range criteria {
		if i >= len(coverage) || strings.TrimSpace(coverage[i]) == "" {
			cov.Uncovered = append(cov.Uncovered, c)
		}
	}
	agg.Coverage = cov
	if len(cov.Uncovered) > 0 {
		agg.Verdict = "FAIL"
		if agg.GateReason == "" {
			if !cov.Attested {
				agg.GateReason = fmt.Sprintf(
					"acceptance coverage: %d criterion(s) not attested — pass acceptance_coverage (one evidence string per criterion, in order)",
					len(criteria))
			} else {
				agg.GateReason = fmt.Sprintf("acceptance coverage: %d of %d criteria uncovered",
					len(cov.Uncovered), len(criteria))
			}
		}
	}
	return agg
}

var (
	scoreRes = map[string]*regexp.Regexp{
		"req":   dimRe("req"),
		"sec":   dimRe("sec"),
		"maint": dimRe("maint"),
	}
	notesRe = regexp.MustCompile(`(?ims)^[ \t]*notes[ \t]*:[ \t]*(.*)`)
)

func dimRe(dim string) *regexp.Regexp {
	// v1: ^[\s*_>#-]*{dim}[\s*_]*:[\s*_]*([0-9]+(?:\.[0-9]+)?) with I|M
	return regexp.MustCompile(`(?im)^[\s*_>#-]*` + dim + `[\s*_]*:[\s*_]*([0-9]+(?:\.[0-9]+)?)`)
}

// ParseScores ports judge.py::parse_scores: last match per dimension wins
// (reasoning models restate scores at the end); nil when any dimension is
// missing. Verdict is derived from the average, never trusted from the model.
func ParseScores(text string) *JudgeResult {
	if text == "" {
		return nil
	}
	find := func(dim string) (float64, bool) {
		ms := scoreRes[dim].FindAllStringSubmatch(text, -1)
		if len(ms) == 0 {
			return 0, false
		}
		f, err := strconv.ParseFloat(ms[len(ms)-1][1], 64)
		return f, err == nil
	}
	req, ok1 := find("req")
	sec, ok2 := find("sec")
	maint, ok3 := find("maint")
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	notes := ""
	if nm := notesRe.FindStringSubmatch(text); nm != nil {
		notes = strings.TrimSpace(nm[1])
	}
	avg := pyRound2((req + sec + maint) / 3)
	verdict := "FAIL"
	if avg >= 8.0 {
		verdict = "PASS"
	}
	return &JudgeResult{Req: req, Sec: sec, Maint: maint, Avg: avg, Verdict: verdict, Notes: notes}
}

// pyRound2 is Python's round(x, 2): round-half-to-EVEN on the exact binary
// value of x. Go's math.Round is half-away-from-zero — a real parity trap at
// values like 8.125 (py → 8.12).
func pyRound2(x float64) float64 {
	r := new(big.Rat).SetFloat64(x)
	r.Mul(r, big.NewRat(100, 1))
	num, den := r.Num(), r.Denom()
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	rem.Abs(rem)
	twice := new(big.Int).Lsh(rem, 1)
	cmp := twice.Cmp(den)
	roundUp := cmp > 0 || (cmp == 0 && q.Bit(0) == 1)
	if roundUp {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	out, _ := new(big.Rat).SetFrac(q, big.NewInt(100)).Float64()
	return out
}

// AggregateResults ports judge.py::aggregate.
func AggregateResults(results []JudgeResult) Aggregate {
	n := float64(len(results))
	var req, sec, maint, avg float64
	for _, j := range results {
		req += j.Req
		sec += j.Sec
		maint += j.Maint
		avg += j.Avg
	}
	agg := Aggregate{
		Req:    pyRound2(req / n),
		Sec:    pyRound2(sec / n),
		Maint:  pyRound2(maint / n),
		Avg:    pyRound2(avg / n),
		Judges: results,
	}
	agg.Verdict = "FAIL"
	if agg.Avg >= 8.0 {
		agg.Verdict = "PASS"
	}
	return agg
}

// ApplyTestGate ports judge.py::apply_test_gate — the sacred invariant
// (ADR-006): failing tests force FAIL regardless of judge scores.
func ApplyTestGate(agg Aggregate, report *TestReport) Aggregate {
	if report == nil {
		return agg
	}
	agg.Tests = report
	if report.ExitCode != 0 {
		cmd := report.Command
		if cmd == "" {
			cmd = "tests"
		}
		agg.Verdict = "FAIL"
		agg.GateReason = fmt.Sprintf(
			"test gate: '%s' exited %d — failing tests force FAIL regardless of judge scores",
			cmd, report.ExitCode)
	}
	return agg
}

// testReportPromptBlock ports judge.py::_test_report_prompt_block, rendering
// the frozen base block plus the structural command/summary lines.
func testReportPromptBlock(report *TestReport) (string, error) {
	if report == nil {
		return "", nil
	}
	status := fmt.Sprintf("RED (exit code %d)", report.ExitCode)
	if report.ExitCode == 0 {
		status = "GREEN (all passing)"
	}
	tpl, err := prompts.Block("judge", 1)
	if err != nil {
		return "", err
	}
	block, err := tpl.Render(map[string]string{"status": status})
	if err != nil {
		return "", err
	}
	if report.Command != "" {
		block += fmt.Sprintf("command: %s\n", report.Command)
	}
	if report.Summary != "" {
		block += fmt.Sprintf("summary: %s\n", report.Summary)
	}
	return block + "\n", nil
}

// buildPrompts assembles the judge messages from the frozen blocks.
func buildPrompts(brief, implementation string, report *TestReport) ([]providers.Message, error) {
	sysTpl, err := prompts.Block("judge", 2)
	if err != nil {
		return nil, err
	}
	system, err := sysTpl.Render(nil)
	if err != nil {
		return nil, err
	}
	testBlock, err := testReportPromptBlock(report)
	if err != nil {
		return nil, err
	}
	userTpl, err := prompts.Block("judge", 3)
	if err != nil {
		return nil, err
	}
	user, err := userTpl.Render(map[string]string{
		"brief":                                  brief,
		"implementation":                         implementation,
		"_test_report_prompt_block(test_report)": testBlock,
	})
	if err != nil {
		return nil, err
	}
	return []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, nil
}

// FormatVerdict ports judge.py::_format_verdict (verdict.md, byte-for-byte —
// python float repr included).
func FormatVerdict(agg Aggregate, nModels int) string {
	fl := store.PyFloatRepr
	var lines []string
	lines = append(lines,
		fmt.Sprintf("=== JUDGE VERDICT (dual-judge, %d models) ===", nModels), "",
		"AGGREGATE",
		fmt.Sprintf("req:   %s", fl(agg.Req)),
		fmt.Sprintf("sec:   %s", fl(agg.Sec)),
		fmt.Sprintf("maint: %s", fl(agg.Maint)),
		fmt.Sprintf("avg:   %s", fl(agg.Avg)),
		fmt.Sprintf("verdict: %s", agg.Verdict),
	)
	if agg.Tests != nil {
		status := fmt.Sprintf("RED (exit %d)", agg.Tests.ExitCode)
		if agg.Tests.ExitCode == 0 {
			status = "GREEN"
		}
		cmd := ""
		if agg.Tests.Command != "" {
			cmd = fmt.Sprintf("  (%s)", agg.Tests.Command)
		}
		lines = append(lines, fmt.Sprintf("tests: %s%s", status, cmd))
	}
	if agg.Coverage != nil {
		covered := len(agg.Coverage.Criteria) - len(agg.Coverage.Uncovered)
		lines = append(lines, fmt.Sprintf("coverage: %d/%d acceptance criteria covered", covered, len(agg.Coverage.Criteria)))
		for _, u := range agg.Coverage.Uncovered {
			lines = append(lines, fmt.Sprintf("  uncovered: %s", u))
		}
	}
	if agg.GateReason != "" {
		lines = append(lines, fmt.Sprintf("gate: %s", agg.GateReason))
	}
	lines = append(lines, "")
	for _, j := range agg.Judges {
		lines = append(lines,
			fmt.Sprintf("--- %s ---", j.ModelKey),
			fmt.Sprintf("req:%s sec:%s maint:%s avg:%s → %s", fl(j.Req), fl(j.Sec), fl(j.Maint), fl(j.Avg), j.Verdict),
		)
		if j.Notes != "" {
			lines = append(lines, fmt.Sprintf("Notes: %s", j.Notes))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

// metricsEntry is v1's build-1.1 record plus the build-2.0 additions
// (user, repo, server_version) — existing fields never change (ADR-005).
type metricsEntry struct {
	Phase      string        `json:"phase"`
	Slug       string        `json:"slug"`
	TaskID     string        `json:"task_id"`
	Task       string        `json:"task"`
	Arm        string        `json:"arm"`
	TaskType   string        `json:"task_type"`
	Pipeline   string        `json:"pipeline"`
	Date       string        `json:"date"`
	ModelsUsed []string      `json:"models_used"`
	Iterations int           `json:"iterations"`
	JudgeScore store.PyFloat `json:"judge_score"`
	ReqScore   store.PyFloat `json:"requirements_score"`
	SecScore   store.PyFloat `json:"security_score"`
	MaintScore store.PyFloat `json:"maintainability_score"`
	Verdict    string        `json:"verdict"`
	TestsGreen *bool         `json:"tests_green"`
	// AcceptanceCovered is nil when the task has no acceptance criteria
	// (additive field, ADR-014); true/false when a coverage gate applied.
	AcceptanceCovered *bool  `json:"acceptance_covered,omitempty"`
	SchemaVer         string `json:"schema_version"`
	Notes             string `json:"notes"`
	User              string `json:"user"`
	Repo              string `json:"repo"`
	ServerVer         string `json:"server_version"`
}

// Deps are the judge stage's collaborators.
type Deps struct {
	Store  *store.Store
	Cfg    *providers.Config
	Caller providers.Caller
	Log    func(string)
	// User and ServerVersion feed the metrics build-2.0 fields.
	User          string
	ServerVersion string
}

// Task ports judge.py::judge_task. Sequential judges (matching v1's request
// order for replay parity), 420s + one retry, max_tokens 32768.
func Task(ctx context.Context, d Deps, projectID, slug, taskID, taskSlug, changedFiles, pipeline, taskLabel string, testReport any, acceptanceCoverage []string) (Aggregate, error) {
	log := d.Log
	if log == nil {
		log = func(string) {}
	}
	if pipeline == "" {
		pipeline = "default"
	}

	// Normalize first: fail fast on a malformed report before burning model calls.
	report, err := NormalizeTestReport(testReport)
	if err != nil {
		return Aggregate{}, err
	}

	briefBytes, err := d.Store.ReadArtifact(projectID, slug, "tasks/"+taskID+"-"+taskSlug+"/plan.md")
	if err != nil {
		return Aggregate{}, fmt.Errorf("task brief missing for %s-%s: run planning first (or pass brief)", taskID, taskSlug)
	}
	brief := string(briefBytes)

	if strings.TrimSpace(changedFiles) == "" {
		return Aggregate{}, fmt.Errorf("no implementation content provided to judge")
	}

	judges, err := d.Cfg.ResolveJudges(pipeline, log)
	if err != nil {
		return Aggregate{}, err
	}

	log(fmt.Sprintf("[judge] task %s: %d judges...", taskID, len(judges)))

	messages, err := buildPrompts(brief, changedFiles, report)
	if err != nil {
		return Aggregate{}, err
	}

	var results []JudgeResult
	for i, j := range judges {
		label := fmt.Sprintf("judge %d/%d (%s)", i+1, len(judges), j.Key)
		req := providers.CallRequest{
			ModelKey: j.Key, ModelID: j.Model.ID, BaseURL: j.Provider.BaseURL,
			EnvKey: j.Provider.EnvKey, Messages: messages,
			MaxTokens: 32768, Timeout: 420 * time.Second, Label: label,
		}
		out, ok := d.Caller.CallModel(ctx, req)
		if !ok {
			retry := req
			retry.Label = label + " (retry)"
			out, ok = d.Caller.CallModel(ctx, retry)
		}
		if !ok {
			log(fmt.Sprintf("[%s] could not parse scores; skipping.", label))
			continue
		}
		parsed := ParseScores(out)
		if parsed == nil {
			log(fmt.Sprintf("[%s] could not parse scores; skipping.", label))
			continue
		}
		parsed.ModelKey = j.Key
		results = append(results, *parsed)
	}

	if len(results) == 0 {
		return Aggregate{}, fmt.Errorf("no judge produced parseable scores")
	}

	agg := AggregateResults(results)
	agg = ApplyTestGate(agg, report)

	// Acceptance-coverage gate (ADR-014): read the task's acceptance criteria
	// and require each covered. Absent/empty acceptance.md → inert.
	accBytes, _ := d.Store.ReadArtifact(projectID, slug, "tasks/"+taskID+"-"+taskSlug+"/acceptance.md")
	agg = ApplyAcceptanceGate(agg, ParseAcceptanceCriteria(string(accBytes)), acceptanceCoverage)

	if err := d.Store.WriteBuildArtifact(projectID, slug, taskID, taskSlug, "verdict.md",
		[]byte(FormatVerdict(agg, len(results)))); err != nil {
		return Aggregate{}, err
	}

	taskIDField := taskLabel
	if taskIDField == "" {
		taskIDField = slug + "/" + taskID
	}
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = r.ModelKey
	}
	var testsGreen *bool
	if agg.Tests != nil {
		green := agg.Tests.ExitCode == 0
		testsGreen = &green
	}
	var acceptanceCovered *bool
	if agg.Coverage != nil {
		fully := len(agg.Coverage.Uncovered) == 0
		acceptanceCovered = &fully
	}
	entry := metricsEntry{
		Phase: "build", Slug: slug, TaskID: taskIDField, Task: taskID, Arm: "build",
		TaskType: "implementation", Pipeline: pipeline,
		Date: time.Now().UTC().Format("2006-01-02"), ModelsUsed: keys, Iterations: 1,
		JudgeScore: store.PyFloat(agg.Avg), ReqScore: store.PyFloat(agg.Req),
		SecScore: store.PyFloat(agg.Sec), MaintScore: store.PyFloat(agg.Maint),
		Verdict: agg.Verdict, TestsGreen: testsGreen, AcceptanceCovered: acceptanceCovered, SchemaVer: "build-2.0",
		Notes: fmt.Sprintf("local-fusion build judge; dual-judge aggregate over %d judges", len(keys)),
		User:  d.User, Repo: projectID, ServerVer: d.ServerVersion,
	}
	if err := d.Store.AppendMetric(entry); err != nil {
		log(fmt.Sprintf("Warning: failed to log outcome to metrics.jsonl: %v", err))
	}

	if err := markJudged(d.Store, projectID, slug, taskID, agg); err != nil {
		log(fmt.Sprintf("Warning: failed to update manifest: %v", err))
	}

	return agg, nil
}

// markJudged ports judge.py::_mark_judged.
func markJudged(s *store.Store, projectID, slug, taskID string, agg Aggregate) error {
	m, err := s.ReadManifest(projectID, slug)
	if err != nil {
		return err
	}
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			m.Tasks[i].Status = "judged:" + agg.Verdict
			m.Tasks[i].Scores = &store.ScoreSet{
				Req: store.PyFloat(agg.Req), Sec: store.PyFloat(agg.Sec),
				Maint: store.PyFloat(agg.Maint), Avg: store.PyFloat(agg.Avg),
			}
		}
	}
	return s.WriteManifest(projectID, slug, m)
}
