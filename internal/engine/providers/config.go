// Package providers ports v1 orchestrator/fusion/common.py: the providers.yaml
// registry (schema unchanged — a v1 file loads unmodified, the port contract)
// and the single provider HTTP client that replaces v1's curl subprocess
// (ADR-008). Resolution semantics are line-for-line from common.py.
package providers

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Provider mirrors one providers.<name> entry. Unknown YAML fields (plan,
// capacity details) are preserved-by-ignoring: v1 files load unmodified.
type Provider struct {
	Enabled *bool  `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
	EnvKey  string `yaml:"env_key"`
}

// IsEnabled ports Python's provider_cfg.get("enabled", True).
func (p Provider) IsEnabled() bool { return p.Enabled == nil || *p.Enabled }

// Model mirrors one models.<key> entry.
type Model struct {
	Provider string             `yaml:"provider"`
	ID       string             `yaml:"id"`
	Roles    []string           `yaml:"roles"`
	Scores   map[string]float64 `yaml:"scores"`
}

// Panel mirrors pipelines.<name>.<role>_panel / judges. The coder_fusion
// block shares the same YAML slot but is flat model-name fields; those decode
// here too (unknown fields are ignored everywhere else).
type Panel struct {
	N         *int     `yaml:"n"`
	Models    []string `yaml:"models"`
	Providers []string `yaml:"providers"`
	CoderA    string   `yaml:"coder_a"`
	CoderB    string   `yaml:"coder_b"`
	Evaluator string   `yaml:"evaluator"`
	Lead      string   `yaml:"lead"`
}

// Pipeline holds the per-role panels, keyed as in YAML (tl_panel,
// reviewer_panel, judges, ...).
type Pipeline map[string]Panel

// Config is the loaded registry.
type Config struct {
	Providers map[string]Provider `yaml:"providers"`
	Models    map[string]Model    `yaml:"models"`
	Pipelines map[string]Pipeline `yaml:"pipelines"`
}

// Load reads providers.yaml (v1 schema).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("providers.yaml: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("providers.yaml: %w", err)
	}
	return &cfg, nil
}

// Resolved is one usable (model key, model, provider) triple.
type Resolved struct {
	Key      string
	Model    Model
	Provider Provider
}

// ResolveNamed ports common.py::resolve_named.
func (c *Config) ResolveNamed(key string) (Resolved, error) {
	m, ok := c.Models[key]
	if !ok {
		return Resolved{}, fmt.Errorf("model '%s' not found in config", key)
	}
	p, ok := c.Providers[m.Provider]
	if !ok {
		return Resolved{}, fmt.Errorf("provider '%s' for model '%s' not found", m.Provider, key)
	}
	if !p.IsEnabled() {
		return Resolved{}, fmt.Errorf("model '%s' provider '%s' is disabled", key, m.Provider)
	}
	return Resolved{Key: key, Model: m, Provider: p}, nil
}

// CoderFusionBlock returns the pipeline's coder_fusion model names with v1
// coder_fusion.py error semantics.
func (c *Config) CoderFusionBlock(pipeline string) (map[string]string, error) {
	pipe, ok := c.Pipelines[pipeline]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' not found in config", pipeline)
	}
	cf, ok := pipe["coder_fusion"]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' has no coder_fusion block; add coder_a/coder_b/evaluator/lead", pipeline)
	}
	return map[string]string{
		"coder_a": cf.CoderA, "coder_b": cf.CoderB,
		"evaluator": cf.Evaluator, "lead": cf.Lead,
	}, nil
}

// ResolveJudges ports judge.py::resolve_judges: the pipeline's judges.models
// list, skipping unknown models and disabled providers; error when none usable.
func (c *Config) ResolveJudges(pipeline string, warn func(string)) ([]Resolved, error) {
	pipe, ok := c.Pipelines[pipeline]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' has no judges configured", pipeline)
	}
	judges, ok := pipe["judges"]
	if !ok || len(judges.Models) == 0 {
		return nil, fmt.Errorf("pipeline '%s' has no judges configured", pipeline)
	}
	var resolved []Resolved
	for _, key := range judges.Models {
		m, ok := c.Models[key]
		if !ok {
			warn(fmt.Sprintf("Warning: judge model '%s' not found in config; skipping.", key))
			continue
		}
		p := c.Providers[m.Provider]
		if !p.IsEnabled() {
			warn(fmt.Sprintf("Warning: judge model '%s' provider disabled; skipping.", key))
			continue
		}
		resolved = append(resolved, Resolved{Key: key, Model: m, Provider: p})
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no usable judge models for pipeline '%s'", pipeline)
	}
	return resolved, nil
}

// ResolveRoleModels ports common.py::resolve_role_models, including the
// explicit-panel path, the tl/reviewer cross-eligibility fallback, the panel
// provider restriction, and the ignore-score last resort.
func (c *Config) ResolveRoleModels(pipeline, role string, n int, warn func(string)) ([]Resolved, error) {
	pipe, ok := c.Pipelines[pipeline]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' not found in config", pipeline)
	}
	panel := pipe[role+"_panel"]
	if n <= 0 {
		if panel.N != nil {
			n = *panel.N
		} else {
			n = 3
		}
	}

	if len(panel.Models) > 0 {
		var scored []candidate
		for _, key := range panel.Models {
			m, ok := c.Models[key]
			if !ok {
				continue
			}
			if !c.Providers[m.Provider].IsEnabled() {
				continue
			}
			if score, ok := m.Scores[role]; ok {
				scored = append(scored, candidate{score, key})
			}
		}
		if len(scored) > 0 {
			sortCands(scored)
			return c.take(scored, n), nil
		}
		warn(fmt.Sprintf("Warning: %s_panel.models have no %s scores; selecting from the registry by score.", role, role))
	}

	candidates := c.roleCandidates(role, panel.Providers, false)
	if len(candidates) == 0 {
		warn(fmt.Sprintf("Warning: no %s models meet score criteria; falling back to any eligible model.", role))
		candidates = c.roleCandidates(role, panel.Providers, true)
	}
	sortCands(candidates)
	return c.take(candidates, n), nil
}

type candidate struct {
	score float64
	key   string
}

func (c *Config) roleCandidates(role string, allowedProviders []string, ignoreScore bool) []candidate {
	var found []candidate
	for _, key := range c.modelKeysSorted() {
		m := c.Models[key]
		roles := m.Roles
		if !contains(roles, role) && !contains(roles, "tl") && !contains(roles, "reviewer") {
			continue
		}
		if !contains(roles, role) {
			// Only tl/reviewer cross-eligibility, and only for those two roles.
			if role != "tl" && role != "reviewer" {
				continue
			}
		}
		if len(allowedProviders) > 0 && !contains(allowedProviders, m.Provider) {
			continue
		}
		if !c.Providers[m.Provider].IsEnabled() {
			continue
		}
		score, ok := m.Scores[role]
		if !ok {
			if s, ok2 := m.Scores["tl"]; ok2 {
				score = s
			} else if s, ok2 := m.Scores["reviewer"]; ok2 {
				score = s
			} else {
				score = 0
			}
		}
		if !ignoreScore && score == 0 {
			continue
		}
		found = append(found, candidate{score, key})
	}
	return found
}

// sortCands: score descending, stable — ties keep input order, matching
// Python's stable sort.
func sortCands(cands []candidate) {
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].score > cands[b].score })
}

func (c *Config) take(cands []candidate, n int) []Resolved {
	out := make([]Resolved, 0, min(n, len(cands)))
	for _, cd := range cands[:min(n, len(cands))] {
		m := c.Models[cd.key]
		out = append(out, Resolved{Key: cd.key, Model: m, Provider: c.Providers[m.Provider]})
	}
	return out
}

// modelKeysSorted gives deterministic iteration (Go maps are unordered;
// Python dicts preserve YAML order — alphabetical is our stable stand-in,
// with ties broken by score sort anyway).
func (c *Config) modelKeysSorted() []string {
	keys := make([]string, 0, len(c.Models))
	for k := range c.Models {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
