// Package mcp is the MCP surface: Streamable HTTP (primary) + stdio (kept),
// exposing the lf_* tools. Governing decisions: ADR-002 (transports, container,
// bearer token, capacity policy), ADR-003 (async submit→poll tool contract),
// ADR-011 (lf_plan intent attestation). Ships in M2.
package mcp
