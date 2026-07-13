package mcp

import (
	"context"
	"encoding/json"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/coder"
	"local-fusion/internal/jobs"
)

type lfCoderFusionIn struct {
	ProjectID string          `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug      string          `json:"slug" jsonschema:"the work slug"`
	TaskID    string          `json:"task_id" jsonschema:"task id from the manifest (e.g. 01)"`
	TaskSlug  string          `json:"task_slug" jsonschema:"task slug from the manifest"`
	Context   string          `json:"context,omitempty" jsonschema:"code context the agent gathered (existing files, conventions)"`
	Pipeline  string          `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	Solo      bool            `json:"solo,omitempty" jsonschema:"single coder instead of the two-coder fusion (evaluator + lead)"`
	Budget    *budgetOverride `json:"budget,omitempty" jsonschema:"optional budget overrides"`
}

// RegisterCoderTool adds lf_coder_fusion (async submit → job_id; poll lf_job).
// Deliberate contract change: update the snapshot in http_test.go.
func RegisterCoderTool(server *sdk.Server, d PlanDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_coder_fusion",
		Description: "Submit async implementation of a planned task (returns a job_id; poll " +
			"with lf_job). Default: two coders implement in parallel, an evaluator picks the " +
			"base and grafts, a lead merges — degrading gracefully at every rung. solo: true " +
			"uses a single coder. The result's proposed files are returned as data; the agent " +
			"applies them to the repo (the server never touches your filesystem). Requires the " +
			"task's plan.md and acceptance.md (run lf_plan first). See docs/tools.md#lf_coder_fusion.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfCoderFusionIn) (*sdk.CallToolResult, any, error) {
		if d.Engine.Cfg == nil {
			return nil, lfPlanOut{OK: false, Error: noConfigMsg}, nil
		}

		budgets := jobs.Budgets{
			MaxWallClock:  15 * time.Minute, // ADR-007 coder-fusion default
			MaxModelCalls: 8,                // 2 coders (+retries) + evaluator + lead (+retry)
		}
		if b := in.Budget; b != nil {
			if b.MaxWallClockSeconds > 0 {
				budgets.MaxWallClock = time.Duration(b.MaxWallClockSeconds) * time.Second
			}
			if b.MaxModelCalls > 0 {
				budgets.MaxModelCalls = b.MaxModelCalls
			}
			if b.MaxTokensTotal > 0 {
				budgets.MaxTokensTotal = b.MaxTokensTotal
			}
		}

		key := jobs.Key{ProjectID: in.ProjectID, Slug: in.Slug, Stage: "coder_fusion", TaskID: in.TaskID}
		engine := d.Engine
		job, existing, err := d.Runner.Submit(key, jobs.Fingerprint(in), budgets,
			func(jobCtx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
				caller := &budgetedCaller{inner: engine.Caller, jc: jc}
				jc.Progress("task " + in.TaskID + ": coding")
				res, err := coder.Task(jobCtx,
					coder.Deps{Store: engine.Store, Cfg: engine.Cfg, Caller: caller, Log: engine.Log,
						User: engine.User, ServerVersion: engine.Ver},
					in.ProjectID, in.Slug, in.TaskID, in.TaskSlug, in.Context, in.Pipeline, in.Solo)
				if err != nil {
					if caller.budgetErr != nil {
						return nil, caller.budgetErr
					}
					return nil, err
				}
				return json.Marshal(res)
			})
		if err != nil {
			return nil, lfPlanOut{OK: false, Error: err.Error()}, nil
		}
		return nil, lfPlanOut{OK: true, JobID: job.ID, Existing: existing, Status: string(job.Status)}, nil
	})
}
