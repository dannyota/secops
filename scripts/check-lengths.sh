#!/usr/bin/env bash
# check-lengths.sh — enforce the doc/code file-length budget (CLAUDE.md §4).
#
# A file that grows unbounded becomes unreadable for the next reader — human OR
# agent (the roadmap hit 2417 lines before it was split out; soar_playbook.go is
# 1947). This flags Markdown docs over MD_MAX lines and Go source files over
# GO_MAX, so oversize is caught in review, not after it has metastasized. History
# always survives in git — split or trim, don't hoard.
#
# Exit 1 on any NEW violation. The GRANDFATHER list carries the known-oversize Go
# files as documented split-debt; it must only shrink. Lint-style, no deps.
set -euo pipefail
cd "$(dirname "$0")/.."

MD_MAX=450   # published Markdown docs (guides/design/tips/README/ROADMAP)
GO_MAX=700   # Go source files (non-test); a bigger file should be split by topic

# docs/commands/*.md are EXEMPT: generated verbatim from the command tree
# (`secopsctl docs generate`), so page size follows the CLI, not authored prose.

# Known-oversize Go files — split-debt, not new bloat. SHRINK this list as the
# files are broken up; a file that drops below GO_MAX is reported as removable.
# (empty — all the original oversize files have been split.)
GRANDFATHER=()

is_grandfathered() {
  local f=$1
  for g in "${GRANDFATHER[@]}"; do [[ "$f" == "$g" ]] && return 0; done
  return 1
}

violations=0

# --- Markdown docs ---
while IFS= read -r f; do
  n=$(wc -l <"$f")
  if (( n > MD_MAX )); then
    echo "DOC TOO LONG  $f: $n lines (max $MD_MAX) — split or trim (history is in git)"
    violations=$((violations + 1))
  fi
done < <(
  { find docs -name '*.md' -not -path 'docs/commands/*'; echo ROADMAP.md; echo README.md; } | sort -u
)

# --- Go source (non-test) ---
while IFS= read -r f; do
  n=$(wc -l <"$f")
  if (( n > GO_MAX )); then
    if is_grandfathered "$f"; then
      echo "note: $f is $n lines (grandfathered split-debt; please break it up)"
    else
      echo "GO FILE TOO LONG  $f: $n lines (max $GO_MAX) — split by topic into a sibling file"
      violations=$((violations + 1))
    fi
  fi
done < <(find . -name '*.go' -not -path './third_party/*' -not -path './.claude/*' -not -path './.git/*' -not -name '*_test.go' | sed 's#^\./##' | sort)

# A grandfathered file that has shrunk below the cap should leave the list.
for g in "${GRANDFATHER[@]}"; do
  if [[ -f "$g" ]] && (( $(wc -l <"$g") <= GO_MAX )); then
    echo "note: $g is now under $GO_MAX lines — remove it from GRANDFATHER in scripts/check-lengths.sh"
  fi
done

if (( violations > 0 )); then
  echo "FAIL: $violations file(s) over the length budget."
  exit 1
fi
echo "OK: all docs <= $MD_MAX lines, all Go source <= $GO_MAX lines (grandfathered: ${#GRANDFATHER[@]})."
