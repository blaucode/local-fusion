package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/version"
)

// NewServer builds the MCP server with the lf_* tool surface. Tools are added
// as their backing components land (jobs, store, engine); the server identity
// and transport plumbing are stable from M2 day one.
func NewServer() *sdk.Server {
	return sdk.NewServer(&sdk.Implementation{
		Name:    "local-fusion",
		Title:   "local-fusion — multi-model quality gate",
		Version: version.Version,
	}, nil)
}
