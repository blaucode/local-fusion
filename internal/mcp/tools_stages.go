package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/judge"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/engine/review"
	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
)

// EngineDeps back the stage engines (review, judge) — the model registry,
// caller, store, and metrics attribution the async stage tools run with.
type EngineDeps struct {
	Store  *store.Store
	Cfg    *providers.Holder // hot-reloadable; nil (or Load()==nil) until providers.yaml loads
	Caller providers.Caller
	Log    func(string)
	User   string // metrics build-2.0 attribution
	Ver    string
}

// config returns the current config snapshot, or nil if unavailable.
func (d EngineDeps) config() *providers.Config {
	if d.Cfg == nil {
		return nil
	}
	return d.Cfg.Load()
}

const noConfigMsg = "providers.yaml not loaded — put your v1 config at the --config path (see docs/configuration.md#providers)"

// RegisterStageTools adds lf_review and lf_judge. As of the ADR-003 amendment
// (2026-07-16) both are ASYNC (submit→poll) — a reviewer panel and a dual
// reasoning-judge round exceed MCP client timeouts. Deliberate contract change:
// update the snapshot in http_test.go in the same commit.
func RegisterStageTools(server *sdk.Server, d PlanDeps) {
	registerLfReview(server, d)
	registerLfJudge(server, d)
}

// ensureBrief implements briefs-as-data (ADR-001 amendment: until the plan
// stage ports, the agent supplies the brief). It creates the slug + task
// manifest entry when absent and writes plan.md.
func ensureBrief(d EngineDeps, projectID, slug, taskID, taskSlug, brief string) error {
	if _, err := d.Store.ReadManifest(projectID, slug); err != nil {
		if _, initErr := d.Store.InitSlug(projectID, slug, brief, "", "", false); initErr != nil && !errors.Is(initErr, store.ErrExists) {
			return initErr
		}
	}
	m, err := d.Store.ReadManifest(projectID, slug)
	if err != nil {
		return err
	}
	found := false
	for _, t := range m.Tasks {
		if t.ID == taskID {
			found = true
			break
		}
	}
	if !found {
		m.Tasks = append(m.Tasks, store.Task{
			ID: taskID, Slug: taskSlug, Title: taskSlug, Deps: []string{}, Status: "planned",
		})
		if err := d.Store.WriteManifest(projectID, slug, m); err != nil {
			return err
		}
	}
	return d.Store.WriteTaskArtifact(projectID, slug, taskID, taskSlug, "plan.md", []byte(brief))
}

type lfReviewIn struct {
	ProjectID    string          `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug         string          `json:"slug" jsonschema:"the work slug"`
	TaskID       string          `json:"task_id" jsonschema:"task id from the manifest (e.g. 01)"`
	TaskSlug     string          `json:"task_slug" jsonschema:"task slug from the manifest"`
	ChangedFiles string          `json:"changed_files" jsonschema:"full content of every changed file, concatenated with path headers"`
	Pipeline     string          `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	Brief        string          `json:"brief,omitempty" jsonschema:"the task brief; required once if planning has not run for this task (briefs-as-data)"`
	Budget       *budgetOverride `json:"budget,omitempty" jsonschema:"optional budget overrides"`
}

// lfReviewResult is the shape lf_review's job writes into job.result (read via
// lf_job when the job is done).
type lfReviewResult struct {
	Critical  int              `json:"critical"`
	Important int              `json:"important"`
	Minor     int              `json:"minor"`
	Findings  []review.Finding `json:"findings,omitempty"`
	ReviewMD  string           `json:"review_md,omitempty"`
}

func registerLfReview(server *sdk.Server, d PlanDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_review",
		Description: "Submit async multi-model code review of an implementation against its " +
			"task brief (returns a job_id; poll lf_job). A reviewer panel runs sequentially, " +
			"so this takes minutes. The result carries findings with severities and the " +
			"review.md artifact. If planning has not run for the task, pass `brief`. " +
			"See docs/tools.md#lf_review.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfReviewIn) (*sdk.CallToolResult, any, error) {
		cfg := d.Engine.config()
		if cfg == nil {
			return nil, lfPlanOut{OK: false, Error: noConfigMsg}, nil
		}
		if in.Brief != "" {
			if err := ensureBrief(d.Engine, in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.Brief); err != nil {
				return nil, lfPlanOut{OK: false, Error: err.Error()}, nil
			}
		}
		budgets := jobs.Budgets{MaxWallClock: 10 * time.Minute, MaxModelCalls: 8}
		applyBudget(&budgets, in.Budget)

		key := jobs.Key{ProjectID: in.ProjectID, Slug: in.Slug, Stage: "review", TaskID: in.TaskID}
		engine := d.Engine
		job, existing, err := d.Runner.Submit(key, jobs.Fingerprint(in), budgets,
			func(jobCtx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
				caller := &budgetedCaller{inner: engine.Caller, jc: jc}
				jc.Progress("task " + in.TaskID + ": reviewing")
				res, err := review.Task(jobCtx,
					review.Deps{Store: engine.Store, Cfg: cfg, Caller: caller, Log: engine.Log},
					in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.ChangedFiles, in.Pipeline)
				if err != nil {
					if caller.budgetErr != nil {
						return nil, caller.budgetErr
					}
					return nil, err
				}
				md, _ := engine.Store.ReadArtifact(in.ProjectID, in.Slug, "build/"+in.TaskID+"-"+in.TaskSlug+"/review.md")
				return json.Marshal(lfReviewResult{Critical: res.Critical, Important: res.Important,
					Minor: res.Minor, Findings: res.Findings, ReviewMD: string(md)})
			})
		if err != nil {
			return nil, lfPlanOut{OK: false, Error: err.Error()}, nil
		}
		return nil, lfPlanOut{OK: true, JobID: job.ID, Existing: existing, Status: string(job.Status)}, nil
	})
}

type lfJudgeIn struct {
	ProjectID          string          `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug               string          `json:"slug" jsonschema:"the work slug"`
	TaskID             string          `json:"task_id" jsonschema:"task id from the manifest (e.g. 01)"`
	TaskSlug           string          `json:"task_slug" jsonschema:"task slug from the manifest"`
	ChangedFiles       string          `json:"changed_files" jsonschema:"full content of every changed file, concatenated with path headers"`
	TestReport         map[string]any  `json:"test_report" jsonschema:"deterministic test evidence: {command, exit_code, summary} from the test runner you just executed"`
	AcceptanceCoverage []string        `json:"acceptance_coverage,omitempty" jsonschema:"one evidence string per acceptance criterion, in the order they appear in the task's acceptance.md (ADR-014): the test or code that covers it. PASS requires every criterion covered. Call once with no coverage to see the criteria list."`
	Pipeline           string          `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	TaskLabel          string          `json:"task_label,omitempty" jsonschema:"metrics label (default: slug/task_id)"`
	Brief              string          `json:"brief,omitempty" jsonschema:"the task brief; required once if planning has not run for this task (briefs-as-data)"`
	Budget             *budgetOverride `json:"budget,omitempty" jsonschema:"optional budget overrides"`
}

// lfJudgeOut is lf_judge's tool response: EITHER a submitted job (job_id) OR an
// immediate escalate_to_human refusal (ADR-003/007 amendment) — the escalate
// check is synchronous (no job, no model calls).
type lfJudgeOut struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	JobID    string `json:"job_id,omitempty"`
	Existing bool   `json:"existing,omitempty"`
	Status   string `json:"status,omitempty"`
	// Synchronous escalate refusal:
	Verdict    string `json:"verdict,omitempty"`
	Escalated  bool   `json:"escalated,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	GateReason string `json:"gate_reason,omitempty"`
}

// lfJudgeResult is what the judge job writes into job.result (read via lf_job).
type lfJudgeResult struct {
	Verdict    string              `json:"verdict"`
	Avg        float64             `json:"avg"`
	Req        float64             `json:"req"`
	Sec        float64             `json:"sec"`
	Maint      float64             `json:"maint"`
	GateReason string              `json:"gate_reason,omitempty"`
	Judges     []judge.JudgeResult `json:"judges,omitempty"`
	VerdictMD  string              `json:"verdict_md,omitempty"`
	Attempt    int                 `json:"attempt"`
	Criteria   []string            `json:"acceptance_criteria,omitempty"`
	Uncovered  []string            `json:"acceptance_uncovered,omitempty"`
}

// maxJudgeAttempts turns v1's "fix, re-judge once, then stop" convention into
// an engine guarantee (ADR-007): two judge rounds run; a third is refused with
// escalate_to_human. The ledger lives in the manifest (Task.JudgeAttempts) and
// is a v2 tool-contract concern — the ported judge engine stays byte-faithful.
const maxJudgeAttempts = 2

// judgeAttempts reads the current per-task attempt count (0 if unknown).
func judgeAttempts(d EngineDeps, projectID, slug, taskID string) int {
	m, err := d.Store.ReadManifest(projectID, slug)
	if err != nil {
		return 0
	}
	for _, t := range m.Tasks {
		if t.ID == taskID {
			return t.JudgeAttempts
		}
	}
	return 0
}

// bumpJudgeAttempts records one completed judge round in the manifest.
func bumpJudgeAttempts(d EngineDeps, projectID, slug, taskID string) int {
	m, err := d.Store.ReadManifest(projectID, slug)
	if err != nil {
		return 0
	}
	n := 0
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			m.Tasks[i].JudgeAttempts++
			n = m.Tasks[i].JudgeAttempts
		}
	}
	_ = d.Store.WriteManifest(projectID, slug, m)
	return n
}

func registerLfJudge(server *sdk.Server, d PlanDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_judge",
		Description: "Submit the async quality gate (returns a job_id; poll lf_job). Dual-judge " +
			"scoring with deterministic test AND acceptance-coverage gates: PASS requires test " +
			"exit_code 0 AND average score >= 8.0 AND every acceptance criterion covered — a red " +
			"test or an uncovered criterion forces FAIL no matter the scores (ADR-006/014). Run " +
			"tests first and pass the report; pass acceptance_coverage (one evidence string per " +
			"criterion). After two judge rounds a third returns escalate_to_human IMMEDIATELY " +
			"(no job, no model calls) — stop and get a person (ADR-007). Otherwise the verdict, " +
			"scores, and coverage land in job.result. See docs/tools.md#lf_judge.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfJudgeIn) (*sdk.CallToolResult, any, error) {
		cfg := d.Engine.config()
		if cfg == nil {
			return nil, lfJudgeOut{OK: false, Error: noConfigMsg}, nil
		}
		if in.Brief != "" {
			if err := ensureBrief(d.Engine, in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.Brief); err != nil {
				return nil, lfJudgeOut{OK: false, Error: err.Error()}, nil
			}
		}
		// Judge-retry ledger (ADR-007): refuse a third attempt synchronously,
		// BEFORE any job or model call — the guarantee stays instant.
		if prior := judgeAttempts(d.Engine, in.ProjectID, in.Slug, in.TaskID); prior >= maxJudgeAttempts {
			return nil, lfJudgeOut{OK: true, Verdict: "escalate_to_human", Escalated: true, Attempt: prior,
				GateReason: fmt.Sprintf("judge-retry ledger: task %s already judged %d times — "+
					"a third attempt escalates to a human instead of re-judging (ADR-007). "+
					"Stop the fix→re-judge loop and get a person to look.", in.TaskID, prior)}, nil
		}
		budgets := jobs.Budgets{MaxWallClock: 15 * time.Minute, MaxModelCalls: 8}
		applyBudget(&budgets, in.Budget)

		key := jobs.Key{ProjectID: in.ProjectID, Slug: in.Slug, Stage: "judge", TaskID: in.TaskID}
		engine := d.Engine
		job, existing, err := d.Runner.Submit(key, jobs.Fingerprint(in), budgets,
			func(jobCtx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
				caller := &budgetedCaller{inner: engine.Caller, jc: jc}
				jc.Progress("task " + in.TaskID + ": judging")
				agg, err := judge.Task(jobCtx,
					judge.Deps{Store: engine.Store, Cfg: cfg, Caller: caller, Log: engine.Log, User: engine.User, ServerVersion: engine.Ver},
					in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.ChangedFiles, in.Pipeline, in.TaskLabel,
					anyOrNil(in.TestReport), in.AcceptanceCoverage)
				if err != nil {
					if caller.budgetErr != nil {
						return nil, caller.budgetErr
					}
					return nil, err
				}
				attempt := bumpJudgeAttempts(engine, in.ProjectID, in.Slug, in.TaskID)
				md, _ := engine.Store.ReadArtifact(in.ProjectID, in.Slug, "build/"+in.TaskID+"-"+in.TaskSlug+"/verdict.md")
				r := lfJudgeResult{Verdict: agg.Verdict, Avg: agg.Avg, Req: agg.Req, Sec: agg.Sec,
					Maint: agg.Maint, GateReason: agg.GateReason, Judges: agg.Judges,
					VerdictMD: string(md), Attempt: attempt}
				if agg.Coverage != nil {
					r.Criteria = agg.Coverage.Criteria
					r.Uncovered = agg.Coverage.Uncovered
				}
				return json.Marshal(r)
			})
		if err != nil {
			return nil, lfJudgeOut{OK: false, Error: err.Error()}, nil
		}
		return nil, lfJudgeOut{OK: true, JobID: job.ID, Existing: existing, Status: string(job.Status)}, nil
	})
}

// anyOrNil keeps v1's "no report" semantics for an absent map argument.
func anyOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return map[string]any(m)
}

// applyBudget overlays optional per-call budget overrides onto defaults.
func applyBudget(b *jobs.Budgets, o *budgetOverride) {
	if o == nil {
		return
	}
	if o.MaxWallClockSeconds > 0 {
		b.MaxWallClock = time.Duration(o.MaxWallClockSeconds) * time.Second
	}
	if o.MaxModelCalls > 0 {
		b.MaxModelCalls = o.MaxModelCalls
	}
	if o.MaxTokensTotal > 0 {
		b.MaxTokensTotal = o.MaxTokensTotal
	}
}
