package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CallRequest is the full request a stage makes — exactly what LF_RECORD
// captures for request parity (ADR-010). Everything the wire payload depends
// on is here; the API key is looked up at send time and never recorded.
type CallRequest struct {
	ModelKey  string        `json:"model_key"`
	ModelID   string        `json:"model_id"`
	BaseURL   string        `json:"base_url"`
	EnvKey    string        `json:"env_key"`
	Messages  []Message     `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Timeout   time.Duration `json:"timeout"`
	Label     string        `json:"label,omitempty"`
}

// Caller is the seam between stages and providers. The HTTP client implements
// it for live runs; the replay harness implements it with canned responses.
//
// Contract ported from v1 common.py::call_model: (content, true) on success —
// including empty content — and ("", false) on ANY failure (timeout, transport,
// API error object, malformed response). Stages degrade on false, never crash.
type Caller interface {
	CallModel(ctx context.Context, req CallRequest) (string, bool)
}

// Client is the live HTTP Caller (replaces v1's curl subprocess).
type Client struct {
	// Env resolves provider env keys (os.Getenv in production; injectable
	// for tests so no real key material enters fixtures).
	Env func(string) string
	// Log receives v1's stderr diagnostics ([label] error: ...).
	Log func(string)
	// HTTPClient is overridable for tests; timeouts come per-call.
	HTTPClient *http.Client

	statsMu sync.Mutex
	stats   map[string]*counter // keyed by base_url
}

type counter struct {
	calls, errors int64
	totalLatency  time.Duration
}

// ProviderStat is a per-provider observability snapshot (baseline service
// observability, M2), keyed by base_url which identifies the provider.
type ProviderStat struct {
	BaseURL      string  `json:"base_url"`
	Calls        int64   `json:"calls"`
	Errors       int64   `json:"errors"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// ProviderStats returns a snapshot of per-provider counters, base_url-sorted
// for stable output.
func (c *Client) ProviderStats() []ProviderStat {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	out := make([]ProviderStat, 0, len(c.stats))
	for base, ct := range c.stats {
		avg := 0.0
		if ct.calls > 0 {
			avg = float64(ct.totalLatency.Milliseconds()) / float64(ct.calls)
		}
		out = append(out, ProviderStat{BaseURL: base, Calls: ct.calls, Errors: ct.errors, AvgLatencyMs: avg})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BaseURL < out[j].BaseURL })
	return out
}

func (c *Client) record(baseURL string, latency time.Duration, ok bool) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	if c.stats == nil {
		c.stats = make(map[string]*counter)
	}
	ct := c.stats[baseURL]
	if ct == nil {
		ct = &counter{}
		c.stats[baseURL] = ct
	}
	ct.calls++
	ct.totalLatency += latency
	if !ok {
		ct.errors++
	}
}

func (c *Client) log(msg string) {
	if c.Log != nil {
		c.Log(msg)
	}
}

// CallModel ports common.py::call_model. Failure never raises — it logs and
// returns false (the v1 "return None" degradation contract).
func (c *Client) CallModel(ctx context.Context, req CallRequest) (content string, ok bool) {
	tag := req.Label
	if tag == "" {
		tag = "call_model"
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 190 * time.Second // v1 default
	}

	start := time.Now()
	defer func() { c.record(trimSlash(req.BaseURL), time.Since(start), ok) }()

	payload, err := json.Marshal(map[string]any{
		"model":      req.ModelID,
		"messages":   req.Messages,
		"max_tokens": req.MaxTokens,
	})
	if err != nil {
		c.log(fmt.Sprintf("[%s] error: %v", tag, err))
		return "", false
	}

	if req.Label != "" {
		c.log(fmt.Sprintf("[%s] calling %s...", req.Label, req.ModelID))
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, "POST",
		trimSlash(req.BaseURL)+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		c.log(fmt.Sprintf("[%s] error: %v", tag, err))
		return "", false
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Env != nil {
		if key := c.Env(req.EnvKey); key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(httpReq)
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			c.log(fmt.Sprintf("[%s] error: request timed out after %s", tag, timeout))
		} else {
			c.log(fmt.Sprintf("[%s] error: %v", tag, err))
		}
		return "", false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		c.log(fmt.Sprintf("[%s] error: empty response", tag))
		return "", false
	}

	var data struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		raw := string(body)
		if len(raw) > 200 {
			raw = raw[:200]
		}
		c.log(fmt.Sprintf("[%s] JSON parse error: %v — raw: %s", tag, err, raw))
		return "", false
	}
	if len(data.Error) > 0 && string(data.Error) != "null" {
		c.log(fmt.Sprintf("[%s] API error: %s", tag, data.Error))
		return "", false
	}
	if len(data.Choices) == 0 || data.Choices[0].Message.Content == nil {
		c.log(fmt.Sprintf("[%s] malformed response (no choices/message/content)", tag))
		return "", false
	}

	if req.Label != "" {
		c.log(fmt.Sprintf("[%s] done", req.Label))
	}
	return *data.Choices[0].Message.Content, true
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
