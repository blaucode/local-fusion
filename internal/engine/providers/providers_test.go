package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fixtureYAML = `
providers:
  featherless:
    enabled: true
    plan:
      name: premium
    base_url: https://api.featherless.ai/v1
    env_key: FEATHERLESS_API_KEY
    capacity:
      model: units
      total: 4
  ollama:
    enabled: true
    base_url: https://ollama.com/v1
    env_key: OLLAMA_API_KEY
  disabledp:
    enabled: false
    base_url: https://x
    env_key: X

models:
  judge-a:
    provider: featherless
    id: vendor/judge-a
    roles: [judge]
    scores: {judge: 9.8}
  judge-b:
    provider: featherless
    id: vendor/judge-b
    roles: [judge, synthesizer]
    scores: {judge: 9.5}
  judge-dead:
    provider: disabledp
    id: x/dead
    roles: [judge]
    scores: {judge: 9.9}
  rev-1:
    provider: ollama
    id: vendor/rev-1
    roles: [reviewer]
    scores: {reviewer: 9.0}
  rev-2:
    provider: ollama
    id: vendor/rev-2
    roles: [tl]
    scores: {tl: 8.5}
  rev-feather:
    provider: featherless
    id: vendor/rev-f
    roles: [reviewer]
    scores: {reviewer: 9.9}

pipelines:
  default:
    judges:
      models: [judge-a, judge-dead, judge-b]
    reviewer_panel:
      n: 2
      providers: [ollama]
`

func loadFixture(t *testing.T) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(path, []byte(fixtureYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadV1Schema(t *testing.T) {
	cfg := loadFixture(t)
	if cfg.Providers["featherless"].BaseURL != "https://api.featherless.ai/v1" {
		t.Fatalf("providers parse: %+v", cfg.Providers["featherless"])
	}
	if !cfg.Providers["ollama"].IsEnabled() { // enabled absent → default true
		t.Fatal("enabled default must be true")
	}
	if cfg.Providers["disabledp"].IsEnabled() {
		t.Fatal("explicit enabled:false ignored")
	}
}

func TestResolveJudgesSkipsDisabledAndKeepsOrder(t *testing.T) {
	cfg := loadFixture(t)
	var warns []string
	judges, err := cfg.ResolveJudges("default", func(s string) { warns = append(warns, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(judges) != 2 || judges[0].Key != "judge-a" || judges[1].Key != "judge-b" {
		t.Fatalf("judges = %+v", judges)
	}
	if len(warns) != 1 {
		t.Fatalf("expected one skip warning, got %v", warns)
	}
	if _, err := cfg.ResolveJudges("nope", func(string) {}); err == nil {
		t.Fatal("unknown pipeline must error")
	}
}

func TestResolveRoleModelsHonorsProviderRestriction(t *testing.T) {
	cfg := loadFixture(t)
	// reviewer_panel restricts to ollama: rev-feather (9.9) must be excluded;
	// rev-1 (reviewer 9.0) then rev-2 (tl fallback 8.5).
	got, err := cfg.ResolveRoleModels("default", "reviewer", 0, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "rev-1" || got[1].Key != "rev-2" {
		t.Fatalf("reviewers = %+v", got)
	}
}

func TestResolveNamed(t *testing.T) {
	cfg := loadFixture(t)
	if _, err := cfg.ResolveNamed("judge-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveNamed("judge-dead"); err == nil {
		t.Fatal("disabled provider must error")
	}
	if _, err := cfg.ResolveNamed("missing"); err == nil {
		t.Fatal("missing model must error")
	}
}

func TestClientCallModelContract(t *testing.T) {
	var gotAuth, gotBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := json.Marshal(map[string]any{"ok": true})
		_ = b
		var payload map[string]any
		body, _ := readAll(r)
		gotBody = body
		_ = json.Unmarshal([]byte(body), &payload)
		switch payload["model"] {
		case "ok/model":
			w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
		case "empty/content":
			w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
		case "api/error":
			w.Write([]byte(`{"error":{"message":"boom"}}`))
		case "malformed/resp":
			w.Write([]byte(`{"choices":[]}`))
		default:
			w.Write([]byte(`not json`))
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := &Client{Env: func(k string) string { return map[string]string{"KEY": "sekrit"}[k] }}
	base := CallRequest{BaseURL: ts.URL + "/v1/", EnvKey: "KEY", MaxTokens: 8192,
		Messages: []Message{{Role: "user", Content: "hi"}}, Timeout: 5 * time.Second}

	req := base
	req.ModelID = "ok/model"
	out, ok := c.CallModel(context.Background(), req)
	if !ok || out != "hello" {
		t.Fatalf("success case: %q %v", out, ok)
	}
	if gotAuth != "Bearer sekrit" {
		t.Fatalf("auth = %q", gotAuth)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(gotBody), &sent); err != nil || sent["max_tokens"] != float64(8192) {
		t.Fatalf("payload = %s", gotBody)
	}

	// v1 contract: empty content is SUCCESS (content present), not a failure.
	req.ModelID = "empty/content"
	out, ok = c.CallModel(context.Background(), req)
	if !ok || out != "" {
		t.Fatalf("empty content must be ok: %q %v", out, ok)
	}

	for _, id := range []string{"api/error", "malformed/resp", "garbage"} {
		req.ModelID = id
		if _, ok := c.CallModel(context.Background(), req); ok {
			t.Fatalf("%s must fail", id)
		}
	}

	// Timeout → false, not panic/hang.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slow.Close()
	req.BaseURL = slow.URL
	req.Timeout = 50 * time.Millisecond
	if _, ok := c.CallModel(context.Background(), req); ok {
		t.Fatal("timeout must fail")
	}
}

func readAll(r *http.Request) (string, error) {
	defer r.Body.Close()
	b := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			return string(b), nil
		}
	}
}
