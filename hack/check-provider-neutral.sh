#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

core_paths=("$repo_dir/main.go" "$repo_dir/internal" "$repo_dir/flake.nix")
source_globs=(--glob '*.go' --glob '*.nix')

# Discover provider names from extras. Git is part of the core vocabulary, so
# its name is too broad to audit as a standalone word.
provider_terms=()
for manifest in "$repo_dir"/extras/*/provider.yaml; do
  provider_name=$(basename "$(dirname "$manifest")")
  if [[ $provider_name != git ]]; then
    provider_terms+=("$provider_name")
  fi
done

# These legacy spellings are not necessarily provider directory names. Keep
# them explicit so removed integrations cannot leak back into core.
legacy_terms=(
  claude_code
  codex_cli_rs
  github-copilot
  oh-my-pi
  'pi cli'
  'fx cli'
  .cursor/
  'cursor cli'
  difftastic
  diff-so-fancy
  git-delta
  'delta viewer'
  github.com/roshbhatia/traces/extras
)

failed=false
check_term() {
  local label=$1
  local match_mode=$2
  local term=$3
  local matches
  local result

  set +e
  matches=$(rg --line-number --ignore-case "$match_mode" \
    "${source_globs[@]}" -- "$term" "${core_paths[@]}")
  result=$?
  set -e

  if [[ $result -eq 0 ]]; then
    printf '%s %q found in Traces core:\n%s\n' "$label" "$term" "$matches" >&2
    failed=true
  elif [[ $result -ne 1 ]]; then
    exit "$result"
  fi
}

for term in "${provider_terms[@]}"; do
  check_term 'provider name' --word-regexp "$term"
done

for term in "${legacy_terms[@]}"; do
  check_term 'provider term' --fixed-strings "$term"
done

if [[ $failed == true ]]; then
  exit 1
fi
