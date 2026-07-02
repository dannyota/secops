#!/usr/bin/env bash
# gen-seo.sh — generate the SEO/GEO artifacts from the docs tree.
#
# docs/sitemap.xml and docs/llms.txt are GENERATED from docs/_sidebar.md (the
# navigation is the single source of truth for what pages exist); llms.txt page
# descriptions are extracted from each page's first paragraph. docs/llms-full.txt
# is regenerated via generate-llms-full.sh. <lastmod> uses the last git commit
# date so a regeneration with no content change stays diff-clean.
#
# --check regenerates into a temp dir and fails when any committed artifact is
# stale (lastmod differences are ignored) — the CI freshness gate.
set -euo pipefail
cd "$(dirname "$0")/.."

check=false
[ "${1:-}" = "--check" ] && check=true

docs="docs"
sidebar="$docs/_sidebar.md"
base="https://secops.danny.vn"
today="$(git log -1 --format=%cd --date=short 2>/dev/null || date +%Y-%m-%d)"

[ -f "$sidebar" ] || { echo "error: $sidebar not found" >&2; exit 1; }

if $check; then
  outdir="$(mktemp -d)"
  trap 'rm -rf "$outdir"' EXIT
else
  outdir="$docs"
fi

# Extract a page's first paragraph after its title (for llms.txt descriptions).
# Skips markup-only and Previous:/Next: navigation lines; truncation to ~140
# chars happens at a word boundary in the caller.
first_para() {
  awk '
    /^#/ { found=1; next }
    found && /^(Previous|Next):/ { next }
    found && /^[^#>|[<]/ && !/^$/ && !/^---/ && !/^```/ {
      gsub(/\*\*/, ""); gsub(/`/, "")
      printf "%s ", $0; count++
      if (count >= 2) exit
    }
    found && /^$/ && count > 0 { exit }
  ' "$1"
}

# --- sitemap.xml -------------------------------------------------------------

sitemap="$outdir/sitemap.xml"
{
  echo '<?xml version="1.0" encoding="UTF-8"?>'
  echo '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'
  echo "  <url>"
  echo "    <loc>${base}/#/</loc>"
  echo "    <lastmod>${today}</lastmod>"
  echo "    <changefreq>weekly</changefreq>"
  echo "  </url>"

  grep -oP '\(([^)]+\.md)\)|\(([^)]+/)\)' "$sidebar" | tr -d '()' | while read -r href; do
    case "$href" in http*) continue ;; esac
    path="${href%.md}"
    path="${path%/}"
    [ -z "$path" ] && continue
    freq="monthly"
    case "$path" in guides/*) freq="weekly" ;; esac
    echo "  <url>"
    echo "    <loc>${base}/#/${path}</loc>"
    echo "    <lastmod>${today}</lastmod>"
    echo "    <changefreq>${freq}</changefreq>"
    echo "  </url>"
  done

  echo '</urlset>'
} > "$sitemap"

# --- llms.txt ------------------------------------------------------------------

llms="$outdir/llms.txt"
{
  echo "# secopsctl"
  echo ""
  echo "> Operate Google SecOps (Chronicle SIEM + Siemplify SOAR) as code — a Go CLI"
  echo "> and unofficial Go SDK. Pull live state, review in git diff, push back."
  echo "> 340+ commands across the SIEM (ADC/OAuth) and SOAR (AppKey) planes."
  echo ""

  while IFS= read -r line; do
    # Section header: - **Title**
    title="$(echo "$line" | sed -n 's/^- \*\*\(.*\)\*\*$/\1/p')"
    if [ -n "$title" ]; then
      echo ""
      echo "## ${title}"
      echo ""
      continue
    fi

    # Link entry: - [Title](href) at any indent.
    if echo "$line" | grep -qP '^\s*- \['; then
      link_title="$(echo "$line" | sed -n 's/.*\[\([^]]*\)\].*/\1/p')"
      href="$(echo "$line" | sed -n 's/.*(\([^)]*\)).*/\1/p')"
      [ -z "$link_title" ] || [ -z "$href" ] && continue

      case "$href" in
        http*) echo "- [${link_title}](${href})"; continue ;;
      esac

      path="${href%.md}"
      path="${path%/}"
      url="${base}/#/${path}"
      [ -z "$path" ] && url="${base}/#/"

      file="$docs/$href"
      [ "$href" = "/" ] && file="$docs/README.md"
      [ -d "$file" ] && file="${file%/}/README.md"
      desc=""
      if [ -f "$file" ]; then
        desc="$(first_para "$file")"
        desc="${desc%%Previous:*}"   # drop an inline Previous:/Next: nav trailer
        desc="${desc% }"
        if [ ${#desc} -gt 140 ]; then
          desc="${desc:0:140}"
          desc="${desc% *}..."
        fi
      fi

      if [ -n "$desc" ]; then
        echo "- [${link_title}](${url}): ${desc}"
      else
        echo "- [${link_title}](${url})"
      fi
    fi
  done < "$sidebar"
  echo ""
} > "$llms"

# --- llms-full.txt -------------------------------------------------------------

if $check; then
  ./scripts/generate-llms-full.sh --check
else
  ./scripts/generate-llms-full.sh
fi

# --- freshness gate ------------------------------------------------------------

if $check; then
  stale=false
  for f in sitemap.xml llms.txt; do
    if ! diff -I '<lastmod>' -q "$outdir/$f" "$docs/$f" >/dev/null 2>&1; then
      echo "stale: docs/$f" >&2
      stale=true
    fi
  done
  if $stale; then
    echo "Run: ./scripts/gen-seo.sh && git add docs/sitemap.xml docs/llms.txt docs/llms-full.txt" >&2
    exit 1
  fi
  echo "OK: SEO artifacts are up to date."
else
  echo "generated: sitemap.xml ($(grep -c '<url>' "$sitemap") URLs), llms.txt ($(wc -l < "$llms") lines)"
fi
