package mcp

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/judge"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/engine/review"
	"local-fusion/internal/store"
)

// EngineDeps back the synchronous stage tools (lf_review, lf_judge).
type EngineDeps struct {
	Store  *store.Store
	Cfg    *providers.Config // nil until providers.yaml is supplied
	Caller providers.Caller
	Log    func(string)
	User   string // metrics build-2.0 attribution
	Ver    string
}

const noConfigMsg = "providers.yaml not loaded — put your v1 config at the --config path (see docs/configuration.md#providers)"

// RegisterStageTools adds lf_review and lf_judge. Deliberate contract change:
// update the snapshot in http_test.go in the same commit.
func RegisterStageTools(server *sdk.Server, d EngineDeps) {
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
	ProjectID    string `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug         string `json:"slug" jsonschema:"the work slug"`
	TaskID       string `json:"task_id" jsonschema:"task id from the manifest (e.g. 01)"`
	TaskSlug     string `json:"task_slug" jsonschema:"task slug from the manifest"`
	ChangedFiles string `json:"changed_files" jsonschema:"full content of every changed file, concatenated with path headers"`
	Pipeline     string `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	Brief        string `json:"brief,omitempty" jsonschema:"the task brief; required once if planning has not run for this task (briefs-as-data)"`
}

type lfReviewOut struct {
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Critical  int              `json:"critical"`
	Important int              `json:"important"`
	Minor     int              `json:"minor"`
	Findings  []review.Finding `json:"findings,omitempty"`
	ReviewMD  string           `json:"review_md,omitempty"`
}

func registerLfReview(server *sdk.Server, d EngineDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_review",
		Description: "Multi-model code review of an implementation against its task brief. " +
			"Synchronous (1-2 minutes). Returns findings with severities and the review.md " +
			"artifact. If planning has not run for the task, pass `brief`. See docs/tools.md#lf_review.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfReviewIn) (*sdk.CallToolResult, any, error) {
		if d.Cfg == nil {
			return nil, lfReviewOut{OK: false, Error: noConfigMsg}, nil
		}
		if in.Brief != "" {
			if err := ensureBrief(d, in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.Brief); err != nil {
				return nil, lfReviewOut{OK: false, Error: err.Error()}, nil
			}
		}
		res, err := review.Task(ctx, review.Deps{Store: d.Store, Cfg: d.Cfg, Caller: d.Caller, Log: d.Log},
			in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.ChangedFiles, in.Pipeline)
		if err != nil {
			return nil, lfReviewOut{OK: false, Error: err.Error()}, nil
		}
		md, _ := d.Store.ReadArtifact(in.ProjectID, in.Slug, "build/"+in.TaskID+"-"+in.TaskSlug+"/review.md")
		return nil, lfReviewOut{OK: true, Critical: res.Critical, Important: res.Important,
			Minor: res.Minor, Findings: res.Findings, ReviewMD: string(md)}, nil
	})
}

type lfJudgeIn struct {
	ProjectID    string         `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug         string         `json:"slug" jsonschema:"the work slug"`
	TaskID       string         `json:"task_id" jsonschema:"task id from the manifest (e.g. 01)"`
	TaskSlug     string         `json:"task_slug" jsonschema:"task slug from the manifest"`
	ChangedFiles string         `json:"changed_files" jsonschema:"full content of every changed file, concatenated with path headers"`
	TestReport   map[string]any `json:"test_report" jsonschema:"deterministic test evidence: {command, exit_code, summary} from the test runner you just executed"`
	Pipeline     string         `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	TaskLabel    string         `json:"task_label,omitempty" jsonschema:"metrics label (default: slug/task_id)"`
	Brief        string         `json:"brief,omitempty" jsonschema:"the task brief; required once if planning has not run for this task (briefs-as-data)"`
}

type lfJudgeOut struct {
	OK         bool                `json:"ok"`
	Error      string              `json:"error,omitempty"`
	Verdict    string              `json:"verdict,omitempty"`
	Avg        float64             `json:"avg,omitempty"`
	Req        float64             `json:"req,omitempty"`
	Sec        float64             `json:"sec,omitempty"`
	Maint      float64             `json:"maint,omitempty"`
	GateReason string              `json:"gate_reason,omitempty"`
	Judges     []judge.JudgeResult `json:"judges,omitempty"`
	VerdictMD  string              `json:"verdict_md,omitempty"`
	Attempt    int                 `json:"attempt,omitempty"`
	Escalated  bool                `json:"escalated,omitempty"`
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

func registerLfJudge(server *sdk.Server, d EngineDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_judge",
		Description: "Dual-judge quality gate with a deterministic test gate: PASS requires " +
			"test exit_code 0 AND average score >= 8.0 — failing tests force FAIL no matter " +
			"what the judges score (ADR-006). Run your tests first and pass the report. " +
			"After two judge rounds on the same task, a third returns escalate_to_human " +
			"instead of judging again (ADR-007) — stop and get a person. " +
			"Synchronous (up to ~7 min with reasoning judges). See docs/tools.md#lf_judge.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfJudgeIn) (*sdk.CallToolResult, any, error) {
		if d.Cfg == nil {
			return nil, lfJudgeOut{OK: false, Error: noConfigMsg}, nil
		}
		if in.Brief != "" {
			if err := ensureBrief(d, in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.Brief); err != nil {
				return nil, lfJudgeOut{OK: false, Error: err.Error()}, nil
			}
		}
		// Judge-retry ledger (ADR-007): refuse a third attempt BEFORE burning
		// model calls, turning v1's "re-judge once, then stop" convention into
		// a guarantee.
		if prior := judgeAttempts(d, in.ProjectID, in.Slug, in.TaskID); prior >= maxJudgeAttempts {
			return nil, lfJudgeOut{OK: true, Verdict: "escalate_to_human", Escalated: true, Attempt: prior,
				GateReason: fmt.Sprintf("judge-retry ledger: task %s already judged %d times — "+
					"a third attempt escalates to a human instead of re-judging (ADR-007). "+
					"Stop the fix→re-judge loop and get a person to look.", in.TaskID, prior)}, nil
		}
		agg, err := judge.Task(ctx,
			judge.Deps{Store: d.Store, Cfg: d.Cfg, Caller: d.Caller, Log: d.Log, User: d.User, ServerVersion: d.Ver},
			in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.ChangedFiles, in.Pipeline, in.TaskLabel,
			anyOrNil(in.TestReport))
		if err != nil {
			return nil, lfJudgeOut{OK: false, Error: err.Error()}, nil
		}
		attempt := bumpJudgeAttempts(d, in.ProjectID, in.Slug, in.TaskID)
		md, _ := d.Store.ReadArtifact(in.ProjectID, in.Slug, "build/"+in.TaskID+"-"+in.TaskSlug+"/verdict.md")
		return nil, lfJudgeOut{OK: true, Verdict: agg.Verdict, Avg: agg.Avg, Req: agg.Req,
			Sec: agg.Sec, Maint: agg.Maint, GateReason: agg.GateReason, Judges: agg.Judges,
			VerdictMD: string(md), Attempt: attempt}, nil
	})
}

// anyOrNil keeps v1's "no report" semantics for an absent map argument.
func anyOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return map[string]any(m)
}
