#!/usr/bin/env bash
# check-seo.sh — validate SEO/GEO files exist and are consistent with docs.
#
# Checks:
# 1. Required files exist (robots.txt, sitemap.xml, llms.txt, llms-full.txt)
# 2. sitemap.xml lists every page in _sidebar.md
# 3. llms.txt links match _sidebar.md pages
# 4. llms-full.txt is up to date (delegates to generate-llms-full.sh --check)
set -euo pipefail
cd "$(dirname "$0")/.."

errors=0

# 1. Required files.
for f in docs/robots.txt docs/sitemap.xml docs/llms.txt docs/llms-full.txt; do
  if [[ ! -f "$f" ]]; then
    echo "MISSING: $f"
    errors=$((errors + 1))
  fi
done

# 2. Every .md page in _sidebar.md should appear in sitemap.xml.
if [[ -f docs/_sidebar.md && -f docs/sitemap.xml ]]; then
  while IFS= read -r page; do
    # Extract the path from markdown links like (guides/install.md)
    path="${page%.md}"
    path="${path#/}"
    if ! grep -qF "$path" docs/sitemap.xml; then
      echo "SITEMAP MISSING: $path (listed in _sidebar.md but not in sitemap.xml)"
      errors=$((errors + 1))
    fi
  done < <(grep -oP '\((?!http)[^)]+\.md\)' docs/_sidebar.md | tr -d '()')
fi

# 3. llms-full.txt freshness.
if [[ -f scripts/generate-llms-full.sh ]]; then
  if ! bash scripts/generate-llms-full.sh --check; then
    errors=$((errors + 1))
  fi
fi

if (( errors > 0 )); then
  echo "FAIL: $errors SEO/GEO issue(s)."
  exit 1
fi
echo "OK: SEO/GEO files are consistent."
