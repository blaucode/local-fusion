package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/store"
)

func TestLfReload(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "providers.yaml")
	one := `
providers:
  p1: {base_url: "https://p1/v1", env_key: K1}
models:
  a: {provider: p1, id: v/a, roles: [judge]}
pipelines:
  default:
    judges: {models: [a]}
`
	if err := os.WriteFile(cfgPath, []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	holder, err := providers.NewHolder(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer()
	RegisterReloadTool(srv, EngineDeps{Store: st, Cfg: holder})
	ts := httptest.NewServer(Handler(srv, HTTPConfig{}))
	defer ts.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "reload-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	reload := func() map[string]any {
		res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "lf_reload"})
		if err != nil || res.IsError {
			t.Fatalf("lf_reload: %v %v", err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var m map[string]any
		json.Unmarshal(raw, &m)
		return m
	}

	// Edit the file to add a second model, then reload picks it up.
	two := `
providers:
  p1: {base_url: "https://p1/v1", env_key: K1}
models:
  a: {provider: p1, id: v/a, roles: [judge]}
  b: {provider: p1, id: v/b, roles: [judge]}
pipelines:
  default:
    judges: {models: [a, b]}
`
	if err := os.WriteFile(cfgPath, []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	out := reload()
	if out["ok"] != true || out["models"] != float64(2) {
		t.Fatalf("reload after valid edit = %v", out)
	}
	if n := len(holder.Load().Models); n != 2 {
		t.Fatalf("live config models = %d, want 2", n)
	}

	// A broken edit is rejected; the previous (2-model) config stays live.
	if err := os.WriteFile(cfgPath, []byte("this: is: not: valid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	out = reload()
	if out["ok"] != false || out["error"] == nil {
		t.Fatalf("broken reload should fail: %v", out)
	}
	if n := len(holder.Load().Models); n != 2 {
		t.Fatalf("bad reload took the config down: models = %d, want 2 retained", n)
	}
}
