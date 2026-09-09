#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$repo_dir/docs"
build_dir=$(mktemp -d)
fixture=$(mktemp -d)
trap 'rm -rf "$build_dir" "$fixture"' EXIT

go build -o "$build_dir/traces" .
python3 "$repo_dir/hack/token-fixture.py" "$fixture/checkout-service"
(cd "$fixture/checkout-service" && go test -v ./internal/auth) > "$fixture/test-output.txt"
jq --rawfile output "$fixture/test-output.txt" --compact-output '.[] | if .spanId == "test" then .attrs.output = $output else . end' "$repo_dir/hack/fixtures/screenshot-spans.json" > "$fixture/token-review.jsonl"

(
  cd "$fixture"
  PATH="$build_dir:$PATH" freeze \
    --execute "traces -once -color always -provider , -file token-review.jsonl -session token-review" \
    --output "$repo_dir/docs/traces.png" \
    --width 1100 \
    --padding 24 \
    --margin 16 \
    --window

  PATH="$build_dir:$PATH" \
    vhs "$repo_dir/hack/traces.tape" --output "$repo_dir/docs/traces.gif"

  PATH="${build_dir}:${PATH}" \
    vhs "${repo_dir}/hack/traces-noninteractive.tape" --output "${repo_dir}/docs/traces-noninteractive.gif"
)
