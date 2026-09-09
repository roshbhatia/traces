#!/usr/bin/env bash
set -euo pipefail
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/bin"
go build -o "$fixture/bin/traces" .
go build -o "$fixture/bin/traces-provider-codex" ./extras/codex
export PATH="$fixture/bin:$PATH"
export HOME="$fixture/home"
export XDG_CONFIG_HOME="$fixture/config"
export XDG_DATA_HOME="$fixture/data"
export XDG_DATA_DIRS="$fixture/data"
export XDG_STATE_HOME="$fixture/state"
export XDG_CACHE_HOME="$fixture/cache"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME/traces/providers/codex"
unset TRACES_PROVIDER_PATH
cp "$repo_dir/extras/codex/provider.yaml" "$XDG_DATA_HOME/traces/providers/codex/provider.yaml"
export TRACES_CONFIG="$repo_dir/examples/local-harnesses/config.yaml"
python3 "$repo_dir/hack/token-fixture.py" "$fixture/checkout-service"
python3 "$repo_dir/hack/transcript-fixture.py" "$fixture/checkout-service" "$HOME/.codex/sessions/token-review.jsonl"
cd "$fixture/checkout-service"
for recipe in local-harnesses portable-report; do
  vhs "$repo_dir/examples/$recipe/demo.tape" --output "$repo_dir/examples/$recipe/demo.gif"
done
