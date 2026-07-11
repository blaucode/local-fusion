// Package providers holds the model-calling clients: openai-compatible (covers
// Featherless, Ollama Cloud, OpenAI, gateways) and anthropic (Messages API),
// plus the registry loaded from providers.yaml (schema preserved from v1) and
// weighted unit/slot concurrency pools. Governing decision: ADR-008. The M1 S1
// spike (Go net/http vs Cloudflare on Featherless) decides whether a curl-exec
// shim is needed behind this interface.
package providers
