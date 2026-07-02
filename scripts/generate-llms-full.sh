#!/usr/bin/env bash
# generate-llms-full.sh — regenerate docs/llms-full.txt from all Markdown docs.
#
# Run before committing docs changes. CI validates the committed file matches
# what this script produces (--check mode).
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="docs/llms-full.txt"
TMP="${OUT}.tmp"

# Ordered list of docs to concatenate (reading order).
FILES=(
  docs/README.md
  docs/guides/install.md
  docs/guides/configure.md
  docs/guides/the-loop.md
  docs/guides/rules.md
  docs/guides/search.md
  docs/guides/gemini.md
  docs/guides/soar-cases.md
  docs/guides/triage.md
  docs/guides/playbooks.md
  docs/guides/content-hub.md
  docs/guides/reconcile.md
  docs/guides/sdk.md
  docs/guides/reference-siem.md
  docs/guides/reference-soar.md
  docs/tips/README.md
  docs/tips/01-secops-as-code.md
  docs/tips/02-architecture-client.md
  docs/tips/03-yara-l-rules.md
  docs/tips/04-reference-lists-data-tables.md
  docs/tips/05-curated-rules.md
  docs/tips/06-dashboards.md
  docs/tips/07-udm-queries.md
  docs/tips/08-feeds-parsers.md
  docs/tips/09-soar-operations.md
  docs/tips/10-llm-and-automation.md
  docs/tips/11-gemini-and-ai.md
  docs/tips/12-parser-extensions.md
  docs/design/architecture.md
  docs/design/surfaces.md
  docs/design/catalog.md
  docs/design/catalog-siem.md
  docs/design/catalog-soar.md
  docs/design/siem.md
  docs/design/soar.md
  docs/design/cli-naming.md
  docs/commands/README.md
)

{
  cat <<'HEADER'
# secopsctl — Full Documentation

> Operate Google SecOps (Chronicle SIEM + Siemplify SOAR) as code — one Go CLI
> and one importable Go SDK. The core loop is pull live state → review the git
> diff → push it back. This file concatenates all documentation for LLM
> ingestion. Source: https://secops.danny.vn

HEADER

  first=true
  for f in "${FILES[@]}"; do
    if [[ ! -f "$f" ]]; then
      echo "WARNING: missing $f" >&2
      continue
    fi
    if $first; then
      first=false
    else
      echo ""
      echo "---"
      echo ""
    fi
    # Source path comment.
    rel="${f#docs/}"
    echo "> Source: ${rel}"
    echo ""
    # Strip mermaid blocks → placeholder.
    sed '/^```mermaid$/,/^```$/{
      /^```mermaid$/c\[Diagram omitted — see the rendered page]
      /^```$/d
      /^```mermaid$/!d
    }' "$f"
  done
} > "$TMP"

if [[ "${1:-}" == "--check" ]]; then
  if ! diff -q "$TMP" "$OUT" >/dev/null 2>&1; then
    echo "FAIL: $OUT is out of date. Run: ./scripts/generate-llms-full.sh"
    rm -f "$TMP"
    exit 1
  fi
  rm -f "$TMP"
  echo "OK: $OUT is up to date."
else
  mv "$TMP" "$OUT"
  lines=$(wc -l < "$OUT")
  echo "Generated $OUT ($lines lines)."
fi
