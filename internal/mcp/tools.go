package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
)

// ProviderStatser exposes per-provider observability counters (the live
// providers.Client implements it; nil in tests that don't exercise providers).
type ProviderStatser interface {
	ProviderStats() []providers.ProviderStat
}

// Deps are the backing components for the lf_* tool surface.
type Deps struct {
	Runner *jobs.Runner
	Store  *store.Store
	Stats  ProviderStatser
}

// JobView is the pollable job shape returned by lf_job and embedded in
// lf_status (ADR-003: {status, progress, partial, result?, error?}).
// Tool outputs deliberately carry NO outputSchema (handlers use Out = any):
// the SDK infers boolean `true` schemas for any-typed fields, and Claude
// Code's client rejects boolean property schemas — its tools fetch fails and
// the whole server is unusable. v1 (FastMCP dict returns) also shipped no
// output schemas; tool result shapes are pinned by tools_test.go instead.
//
// Within the views: Partial/Result/manifest are `any`, not json.RawMessage —
// RawMessage would also mis-infer (as "array") wherever schemas ARE generated.
type JobView struct {
	JobID       string         `json:"job_id"`
	Stage       string         `json:"stage"`
	TaskID      string         `json:"task_id,omitempty"`
	Attempt     int            `json:"attempt"`
	Status      jobs.Status    `json:"status"`
	Progress    string         `json:"progress,omitempty"`
	Partial     any            `json:"partial,omitempty"`
	Result      any            `json:"result,omitempty"`
	Error       *jobs.JobError `json:"error,omitempty"`
	ModelCalls  int            `json:"model_calls"`
	TokensTotal int            `json:"tokens_total"`
	SubmittedAt time.Time      `json:"submitted_at"`
}

func rawToAny(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(r, &v); err != nil {
		return string(r)
	}
	return v
}

func viewOf(j jobs.Job) JobView {
	return JobView{
		JobID: j.ID, Stage: j.Key.Stage, TaskID: j.Key.TaskID, Attempt: j.Attempt,
		Status: j.Status, Progress: j.Progress, Partial: rawToAny(j.Partial), Result: rawToAny(j.Result),
		Error: j.Error, ModelCalls: j.ModelCalls, TokensTotal: j.TokensTotal,
		SubmittedAt: j.SubmittedAt,
	}
}

// RegisterTools adds the lf_* tools backed by deps. Tool additions are
// deliberate contract changes — the snapshot test in http_test.go must be
// updated in the same commit.
func RegisterTools(server *sdk.Server, deps Deps) {
	registerLfJob(server, deps)
	registerLfCancel(server, deps)
	registerLfStatus(server, deps)
}

// findJob prefers the live runner, falling back to persisted snapshots for
// jobs that predate a server restart (ADR-003 amendment: rediscovery).
func findJob(deps Deps, id string) (jobs.Job, bool) {
	if job, ok := deps.Runner.Get(id); ok {
		return job, true
	}
	if deps.Store != nil {
		return deps.Store.LoadJob(id)
	}
	return jobs.Job{}, false
}

type lfJobIn struct {
	JobID string `json:"job_id" jsonschema:"the job_id returned by an async submit (lf_plan, lf_coder_fusion)"`
}

type lfJobOut struct {
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	Job   *JobView `json:"job,omitempty"`
}

func registerLfJob(server *sdk.Server, deps Deps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_job",
		Description: "Poll an async job. Returns {status, progress, partial, result?, error?}. " +
			"Statuses: queued|running|done|failed|cancelled|budget_exhausted. " +
			"Poll every 30-60s until the status is terminal. See docs/tools.md#lf_job.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfJobIn) (*sdk.CallToolResult, any, error) {
		job, ok := findJob(deps, in.JobID)
		if !ok {
			return nil, lfJobOut{OK: false, Error: "unknown job_id " + in.JobID + " — list jobs with lf_status (docs/tools.md#lf_status)"}, nil
		}
		view := viewOf(job)
		return nil, lfJobOut{OK: true, Job: &view}, nil
	})
}

type lfCancelIn struct {
	JobID string `json:"job_id" jsonschema:"the job to cancel"`
}

type lfCancelOut struct {
	OK        bool        `json:"ok"`
	Cancelled bool        `json:"cancelled"`
	Status    jobs.Status `json:"status,omitempty"`
	Error     string      `json:"error,omitempty"`
}

func registerLfCancel(server *sdk.Server, deps Deps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_cancel",
		Description: "Cooperatively cancel a running job. Artifacts and partial results " +
			"written so far are preserved. Cancelling a finished job is a no-op. " +
			"See docs/tools.md#lf_cancel.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfCancelIn) (*sdk.CallToolResult, any, error) {
		if deps.Runner.Cancel(in.JobID) {
			return nil, lfCancelOut{OK: true, Cancelled: true}, nil
		}
		if job, ok := findJob(deps, in.JobID); ok {
			return nil, lfCancelOut{OK: true, Cancelled: false, Status: job.Status,
				Error: "job already terminal — nothing to cancel"}, nil
		}
		return nil, lfCancelOut{OK: false, Cancelled: false,
			Error: "unknown job_id " + in.JobID + " — list jobs with lf_status (docs/tools.md#lf_status)"}, nil
	})
}

type lfStatusIn struct {
	ProjectID string `json:"project_id" jsonschema:"opaque project identifier (use the repo name)"`
	Slug      string `json:"slug" jsonschema:"the work slug"`
}

type lfStatusOut struct {
	OK           bool                     `json:"ok"`
	Error        string                   `json:"error,omitempty"`
	Manifest     any                      `json:"manifest,omitempty"`
	Jobs         []JobView                `json:"jobs"`
	Providers    []providers.ProviderStat `json:"providers,omitempty"`
	Constitution bool                     `json:"constitution_active"` // ADR-012
}

func registerLfStatus(server *sdk.Server, deps Deps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_status",
		Description: "Manifest plus all known jobs for (project_id, slug) — including jobs " +
			"submitted before a server restart. Use it to rediscover job_ids after an agent " +
			"crash. A missing manifest is not an error while planning hasn't run. " +
			"See docs/tools.md#lf_status.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, in lfStatusIn) (*sdk.CallToolResult, any, error) {
		out := lfStatusOut{OK: true, Jobs: []JobView{}}

		seen := map[string]bool{}
		for _, j := range deps.Runner.List() {
			if j.Key.ProjectID == in.ProjectID && j.Key.Slug == in.Slug {
				out.Jobs = append(out.Jobs, viewOf(j))
				seen[j.ID] = true
			}
		}
		if deps.Store != nil {
			if persisted, err := deps.Store.LoadJobs(); err == nil {
				for _, j := range persisted {
					if !seen[j.ID] && j.Key.ProjectID == in.ProjectID && j.Key.Slug == in.Slug {
						out.Jobs = append(out.Jobs, viewOf(j))
					}
				}
			}
			if m, err := deps.Store.ReadManifest(in.ProjectID, in.Slug); err == nil {
				out.Manifest = m
			}
			out.Constitution = strings.TrimSpace(deps.Store.ReadConstitution(in.ProjectID)) != ""
		}
		sort.Slice(out.Jobs, func(a, b int) bool {
			return out.Jobs[a].SubmittedAt.Before(out.Jobs[b].SubmittedAt)
		})
		if deps.Stats != nil {
			out.Providers = deps.Stats.ProviderStats()
		}
		return nil, out, nil
	})
}
