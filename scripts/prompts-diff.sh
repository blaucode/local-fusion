#!/usr/bin/env bash
# Prompt-freeze check (ADR-008), two layers:
#   1. ALWAYS: verify prompts/*.tmpl match prompts/checksums.sha256 (drift inside this repo).
#   2. IF the v1 repo is reachable (V1_DIR, default ../../vendo/local-fusion): re-run the
#      extraction and byte-diff against prompts/ (drift against the source of truth).
# Exit 0 = frozen. Non-zero = drift; the diff tells you where.
set -euo pipefail
cd "$(dirname "$0")/.."

V1_DIR="${V1_DIR:-../../vendo/local-fusion}"

# Layer 1 — checksums (works everywhere, incl. CI without the v1 checkout)
( cd prompts && sha256sum -c checksums.sha256 --quiet ) \
  || { echo "❌ prompts/ differ from checksums.sha256 — prompt files were edited by hand?"; exit 1; }
echo "layer 1 OK: prompts match committed checksums"

# Layer 2 — re-extraction vs v1 (source of truth)
if [ -d "$V1_DIR/orchestrator/fusion" ]; then
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  python3 scripts/extract-prompts.py --v1 "$V1_DIR" --out "$tmp" >/dev/null
  diff -ru prompts "$tmp" \
    || { echo "❌ prompts/ differ from a fresh v1 extraction — v1 prompts changed, or extractor changed"; exit 1; }
  echo "layer 2 OK: byte-identical to fresh v1 extraction"
else
  echo "layer 2 skipped: v1 repo not found at $V1_DIR (set V1_DIR to enable)"
fi
