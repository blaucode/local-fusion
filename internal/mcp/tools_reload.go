package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type lfReloadOut struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Path      string `json:"path,omitempty"`
	Models    int    `json:"models,omitempty"`
	Pipelines int    `json:"pipelines,omitempty"`
	Providers int    `json:"providers,omitempty"`
	Note      string `json:"note,omitempty"`
}

// RegisterReloadTool adds lf_reload — hot-reload the providers.yaml registry
// (R8). A bad edit is rejected and the previous config kept, so a typo never
// takes the gate down. Deliberate contract change: update the http_test.go
// snapshot in the same commit.
func RegisterReloadTool(server *sdk.Server, d EngineDeps) {
	sdk.AddTool(server, &sdk.Tool{
		Name: "lf_reload",
		Description: "Hot-reload providers.yaml (models, pipelines, enable/disable) without a " +
			"restart. A config that fails to parse is rejected and the previous one kept. " +
			"Note: API keys arrive via the container's environment and still require a " +
			"restart to change. See docs/tools.md#lf_reload.",
	}, func(ctx context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
		if d.Cfg == nil {
			return nil, lfReloadOut{OK: false, Error: noConfigMsg}, nil
		}
		cfg, err := d.Cfg.Reload()
		if err != nil {
			note := "kept the previously loaded config"
			if cfg == nil {
				note = "no config is currently active"
			}
			return nil, lfReloadOut{OK: false, Error: err.Error(), Path: d.Cfg.Path(), Note: note}, nil
		}
		return nil, lfReloadOut{OK: true, Path: d.Cfg.Path(),
			Models: len(cfg.Models), Pipelines: len(cfg.Pipelines), Providers: len(cfg.Providers)}, nil
	})
}
