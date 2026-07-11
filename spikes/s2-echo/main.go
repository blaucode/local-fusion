// S2 spike (M1, Q3 / ADR-002): official Go MCP SDK over Streamable HTTP.
// Throwaway evidence code — exposes one tool, lf_echo, plus GET /healthz.
// PASS bar: Claude Code, Cline (the bar), and Cursor each call lf_echo by URL.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoIn struct {
	Text string `json:"text" jsonschema:"the text to echo back"`
}

type echoOut struct {
	Echo   string `json:"echo"`
	Server string `json:"server"`
}

func main() {
	addr := flag.String("addr", ":8484", "listen address")
	flag.Parse()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "lf-spike-s2",
		Title:   "local-fusion S2 echo spike",
		Version: "0.0.1",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "lf_echo",
		Description: "Echo the given text back, prefixed with the server name. Spike tool for MCP connectivity verification.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoOut, error) {
		return nil, echoOut{Echo: in.Text, Server: "lf-spike-s2"}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("lf-spike-s2 listening on %s (MCP at /mcp)", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
