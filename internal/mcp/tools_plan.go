package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/plan"
	"local-fusion/internal/engine/providers"
	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
)

// PlanDeps back lf_plan: the engine deps plus the job runner (async, ADR-003).
type PlanDeps struct {
	Engine EngineDeps
	Runner *jobs.Runner
}

// Attestation subfields are schema-optional on purpose: enforcement lives in
// the handler so a missing/incomplete attestation gets the ADR-011 refusal
// message (which teaches the contract) instead of a bare schema error.
type gitStateIn struct {
	Branch     string `json:"branch,omitempty" jsonschema:"the feature branch the agent created for this slug"`
	BaseBranch string `json:"base_branch,omitempty" jsonschema:"the branch the feature branch was created from"`
	Clean      bool   `json:"clean,omitempty" jsonschema:"true only if the working tree was clean when the branch was created"`
}

type intentIn struct {
	Tier       string `json:"tier,omitempty" jsonschema:"feature | fix | chore"`
	Ref        string `json:"ref,omitempty" jsonschema:"PRD path/URL, issue link, or charter id"`
	ApprovedBy string `json:"approved_by,omitempty" jsonschema:"human identifier who owns this intent"`
	DraftedBy  string `json:"drafted_by,omitempty" jsonschema:"human | agent — authorship is free, ownership is not"`
}

type lfPlanIn struct {
	ProjectID string          `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug      string          `json:"slug" jsonschema:"the work slug (kebab-case)"`
	Request   string          `json:"request" jsonschema:"the feature/fix request text"`
	Context   string          `json:"context,omitempty" jsonschema:"code context the agent gathered (file contents, conventions)"`
	Pipeline  string          `json:"pipeline,omitempty" jsonschema:"pipeline name (default: default)"`
	NoFusion  bool            `json:"no_fusion,omitempty" jsonschema:"skip the TL panel + synthesizer (plan-solo); default false = full deliberation, matching v1"`
	Force     bool            `json:"force,omitempty" jsonschema:"overwrite an existing slug"`
	GitState  *gitStateIn     `json:"git_state,omitempty" jsonschema:"REQUIRED attestation that the agent created the branch on a clean tree (ADR-004)"`
	Intent    *intentIn       `json:"intent,omitempty" jsonschema:"REQUIRED human-owned intent attestation (ADR-011); the loop refuses to run without it"`
	Budget    *budgetOverride `json:"budget,omitempty" jsonschema:"optional budget overrides"`
}

type budgetOverride struct {
	MaxWallClockSeconds int `json:"max_wall_clock_seconds,omitempty" jsonschema:"wall-clock budget in seconds (default 1800 per expected task, capped)"`
	MaxModelCalls       int `json:"max_model_calls,omitempty" jsonschema:"step cap (default 40)"`
	MaxTokensTotal      int `json:"max_tokens_total,omitempty" jsonschema:"token budget (0 = unlimited)"`
}

type lfPlanOut struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	JobID    string `json:"job_id,omitempty"`
	Existing bool   `json:"existing,omitempty"`
	Status   string `json:"status,omitempty"`
}

const intentRefusal = "lf_plan refuses to run without human-owned intent (ADR-011). Provide " +
	"intent: {tier, ref, approved_by, drafted_by} where tier is one of: " +
	"\"feature\" (ref = PRD/ADR path or URL), \"fix\" (ref = approved brief or issue link), " +
	"\"chore\" (ref = a standing charter id present in the store). " +
	"Machines may draft intent; a human must own it. See docs/tools.md#lf_plan."

// validateIntent enforces ADR-011 rules 1 and 4.
func validateIntent(st *store.Store, in *intentIn) error {
	if in == nil || in.Tier == "" || in.Ref == "" || in.ApprovedBy == "" || in.DraftedBy == "" {
		return fmt.Errorf("%s", intentRefusal)
	}
	switch in.Tier {
	case "feature", "fix":
		// Attestation only — content judgment stays human (ADR-011 option C).
	case "chore":
		if _, err := st.CheckCharter(in.Ref); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown intent tier %q — %s", in.Tier, intentRefusal)
	}
	switch in.DraftedBy {
	case "human", "agent":
	default:
		return fmt.Errorf("intent.drafted_by must be \"human\" or \"agent\", got %q", in.DraftedBy)
	}
	return nil
}

// requestMD renders request.md: the request plus the attestation audit block
// (ADR-011: fabrication is auditable).
func requestMD(request string, git gitStateIn, intent intentIn) string {
	return request + fmt.Sprintf(
		"\n\n---\nintent: tier=%s ref=%s approved_by=%s drafted_by=%s\ngit_state: branch=%s base=%s clean=%t\n",
		intent.Tier, intent.Ref, intent.ApprovedBy, intent.DraftedBy,
		git.Branch, git.BaseBranch, git.Clean)
}

// RegisterPlanTool adds lf_plan (async submit → job_id; poll with lf_job).
func RegisterPlanTool(server *sdk.Server, d PlanDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_plan",
		Description: "Submit async planning for a slug (returns a job_id in <2s; poll with " +
			"lf_job every 30-60s). Decomposes the request into tasks and deliberates a " +
			"per-task implementation brief. REQUIRES: git_state attestation (create " +
			"feature/<slug> from a clean tree first) and intent attestation " +
			"{tier, ref, approved_by, drafted_by} — the loop refuses goal-free runs. " +
			"Idempotent: resubmitting identical work returns the running job. " +
			"See docs/tools.md#lf_plan.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfPlanIn) (*sdk.CallToolResult, any, error) {
		cfg := d.Engine.config()
		if cfg == nil {
			return nil, lfPlanOut{OK: false, Error: noConfigMsg}, nil
		}
		if err := validateIntent(d.Engine.Store, in.Intent); err != nil {
			return nil, lfPlanOut{OK: false, Error: err.Error()}, nil
		}
		if in.GitState == nil || in.GitState.Branch == "" || !in.GitState.Clean {
			return nil, lfPlanOut{OK: false, Error: "lf_plan requires a git_state attestation " +
				"{branch, base_branch, clean: true} — create feature/<slug> from a clean tree " +
				"first (ADR-004); the server never touches your repo. See docs/tools.md#lf_plan."}, nil
		}
		if in.Request == "" {
			return nil, lfPlanOut{OK: false, Error: "request must not be empty"}, nil
		}

		intent := store.Intent(*in.Intent)
		git := *in.GitState

		budgets := jobs.Budgets{
			MaxWallClock: 30 * time.Minute,
			// Full plan: decompose + (3 haft + panel(≤3) + synthesize) × ≤8
			// tasks; headroom on top (ADR-007 defaults tune with pilot data).
			MaxModelCalls: 80,
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

		key := jobs.Key{ProjectID: in.ProjectID, Slug: in.Slug, Stage: "plan", TaskID: ""}
		fingerprint := jobs.Fingerprint(in)

		engine := d.Engine
		job, existing, err := d.Runner.Submit(key, fingerprint, budgets,
			func(jobCtx context.Context, jc *jobs.JobContext) (json.RawMessage, error) {
				caller := &budgetedCaller{inner: engine.Caller, jc: jc}
				run := plan.Full // v1 default; no_fusion flips to solo
				if in.NoFusion {
					run = plan.Solo
				}
				res, err := run(jobCtx,
					plan.Deps{Store: engine.Store, Cfg: cfg, Caller: caller, Log: engine.Log},
					jc.Progress,
					in.ProjectID, in.Slug, in.Request, requestMD(in.Request, git, *in.Intent),
					in.Context, in.Pipeline, git.BaseBranch, git.Branch, intent, in.Force)
				if err != nil {
					if caller.budgetErr != nil {
						// The step/token cap fired mid-stage; classify as
						// budget_exhausted, not a generic stage failure.
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

// budgetedCaller charges every model call against the job's step cap before
// delegating (ADR-007: the runner owns the law; the stage merely calls). It
// remembers the budget error so the stage wrapper can classify the failure.
type budgetedCaller struct {
	inner     providers.Caller
	jc        *jobs.JobContext
	budgetErr error
}

func (b *budgetedCaller) CallModel(ctx context.Context, req providers.CallRequest) (string, bool) {
	if err := b.jc.StartModelCall(); err != nil {
		b.budgetErr = err
		return "", false
	}
	return b.inner.CallModel(ctx, req)
}
