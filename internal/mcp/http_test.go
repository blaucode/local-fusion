package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"local-fusion/internal/jobs"
	"local-fusion/internal/store"
	"local-fusion/internal/version"
)

func TestValidateBind(t *testing.T) {
	cases := []struct {
		addr, token string
		insecure    bool
		ok          bool
	}{
		{"127.0.0.1:8484", "", false, true},
		{"localhost:8484", "", false, true},
		{"[::1]:8484", "", false, true},
		{"0.0.0.0:8484", "", false, false},
		{":8484", "", false, false}, // all interfaces
		{"192.168.1.20:8484", "", false, false},
		{"0.0.0.0:8484", "secret", false, true},
		{":8484", "", true, true}, // container CMD: explicit override
		{"not-an-addr", "secret", false, false},
	}
	for _, c := range cases {
		err := ValidateBind(c.addr, c.token, c.insecure)
		if (err == nil) != c.ok {
			t.Errorf("ValidateBind(%q, token=%v, insecure=%v) = %v, want ok=%v", c.addr, c.token != "", c.insecure, err, c.ok)
		}
	}
}

func TestHealthzOpenAndTokenGuard(t *testing.T) {
	h := Handler(NewServer(), HTTPConfig{Token: "s3cret"})
	ts := httptest.NewServer(h)
	defer ts.Close()

	// healthz is always open (skill pre-submit check).
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("healthz = %d %q, want 200 ok", resp.StatusCode, body)
	}

	// /mcp without the token → 401; wrong token → 401.
	for _, auth := range []string{"", "Bearer wrong"} {
		req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader("{}"))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("auth %q: /mcp = %d, want 401", auth, resp.StatusCode)
		}
	}
}

// TestStreamableHTTPContract runs a real SDK client against the handler:
// initialize must report the server identity; the tool list is the contract
// surface (snapshot grows as lf_* tools land).
func TestStreamableHTTPContract(t *testing.T) {
	token := "s3cret"
	srv := NewServer()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := jobs.NewRunner(2, st, nil)
	defer runner.Close()
	RegisterTools(srv, Deps{Runner: runner, Store: st})

	ts := httptest.NewServer(Handler(srv, HTTPConfig{Token: token}))
	defer ts.Close()

	authed := &http.Client{Transport: &bearerTransport{token: token}}
	client := sdk.NewClient(&sdk.Implementation{Name: "contract-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: authed,
	}, nil)
	if err != nil {
		t.Fatalf("connect over Streamable HTTP: %v", err)
	}
	defer session.Close()

	init := session.InitializeResult()
	if init.ServerInfo.Name != "local-fusion" || init.ServerInfo.Version != version.Version {
		t.Fatalf("server identity = %s/%s, want local-fusion/%s",
			init.ServerInfo.Name, init.ServerInfo.Version, version.Version)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	// Contract snapshot (testing strategy: a tool-surface change is a
	// reviewable diff). Update deliberately, in the same commit as the change.
	want := []string{"lf_cancel", "lf_job", "lf_status"}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if !slices.Equal(names, want) {
		t.Fatalf("tool surface = %v, want %v — update the contract snapshot deliberately", names, want)
	}
}

type bearerTransport struct{ token string }

func (b *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}
