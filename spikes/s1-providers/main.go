// S1 spike (M1, Q1 / ADR-001): does Go's net/http pass Featherless/Cloudflare?
// v1 abandoned urllib for curl (403 / Cloudflare error 1010, JA3 fingerprinting).
//
// Two evidence levels, per claim-labeling discipline:
//   - no API key in env → unauthenticated POST; a clean HTTP 401/4xx JSON from the
//     API origin proves the TLS handshake passed the Cloudflare edge (partial PASS);
//     a Cloudflare block page (error 1010/1020, cf HTML) is a FAIL signal.
//   - key present → one real chat completion (max_tokens 16) = full PASS.
//
// Keys are read from env only and never printed.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type target struct {
	name    string
	baseURL string // matches v1 orchestrator/run.py PROVIDERS
	envKey  string
	model   string
}

var targets = []target{
	{"featherless", "https://api.featherless.ai/v1", "FEATHERLESS_API_KEY", "openai/gpt-oss-120b"},
	{"ollama", "https://ollama.com/v1", "OLLAMA_API_KEY", "gpt-oss:120b"},
}

func main() {
	client := &http.Client{Timeout: 120 * time.Second}
	fail := false
	for _, t := range targets {
		if !probe(client, t) {
			fail = true
		}
	}
	if fail {
		os.Exit(1)
	}
}

func probe(client *http.Client, t target) bool {
	key := os.Getenv(t.envKey)
	authed := key != ""

	payload := map[string]any{
		"model":      t.model,
		"max_tokens": 16,
		"messages":   []map[string]string{{"role": "user", "content": "Reply with the single word: pong"}},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", t.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("[%s] request build error: %v\n", t.name, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if authed {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[%s] FAIL: transport error (TLS/conn): %v\n", t.name, err)
		return false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	elapsed := time.Since(start).Round(time.Millisecond)

	snippet := strings.ReplaceAll(string(raw), "\n", " ")
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	cfRay := resp.Header.Get("Cf-Ray")
	server := resp.Header.Get("Server")
	fmt.Printf("[%s] authed=%v status=%d server=%q cf-ray=%q t=%v\n  body: %s\n",
		t.name, authed, resp.StatusCode, server, cfRay, elapsed, snippet)

	blocked := resp.StatusCode == 403 &&
		(strings.Contains(snippet, "1010") || strings.Contains(snippet, "1020") ||
			strings.Contains(strings.ToLower(snippet), "cloudflare"))
	if blocked {
		fmt.Printf("[%s] FAIL: Cloudflare block page — Go TLS fingerprint rejected\n", t.name)
		return false
	}

	if !authed {
		jsonFromOrigin := json.Valid(raw)
		if jsonFromOrigin && resp.StatusCode >= 400 && resp.StatusCode < 500 {
			fmt.Printf("[%s] PARTIAL PASS: origin answered with JSON %d — TLS + Cloudflare edge passed; set %s for full completion test\n",
				t.name, resp.StatusCode, t.envKey)
			return true
		}
		fmt.Printf("[%s] INCONCLUSIVE: unauthenticated response not clearly from API origin\n", t.name)
		return false
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || resp.StatusCode != 200 || len(parsed.Choices) == 0 {
		fmt.Printf("[%s] FAIL: authed completion did not succeed (status %d)\n", t.name, resp.StatusCode)
		return false
	}
	fmt.Printf("[%s] FULL PASS: completion returned %q via plain net/http\n", t.name, strings.TrimSpace(parsed.Choices[0].Message.Content))
	return true
}
