#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$repo_dir/docs"
build_dir=$(mktemp -d)
fixture=$(mktemp)
trap 'rm -rf "$build_dir" "$fixture"' EXIT

go build -o "$build_dir/traces" .
printf '%s\n' \
  '{"traceId":"demo","spanId":"root","name":"Rewrite the renderer","service":"codex","session":"demo","startUnixNano":"1788278400000000000","endUnixNano":"1788278408000000000","attrs":{"prompt":"Move the renderer behind a provider contract"}}' \
  '{"traceId":"demo","spanId":"plan","parentId":"root","name":"Plan provider contract","service":"codex","session":"demo","startUnixNano":"1788278401000000000","endUnixNano":"1788278403000000000"}' \
  '{"traceId":"demo","spanId":"edit","parentId":"root","name":"Edit","service":"codex","session":"demo","startUnixNano":"1788278403000000000","endUnixNano":"1788278407000000000","attrs":{"output":"## internal/ui/diff.go\\n\\n@@ -1 +1 @@\\n-old renderer\\n+provider renderer\\n"}}' \
  > "$fixture"

PATH="$build_dir:$PATH" freeze \
  --execute "traces -once -color always -provider , -file $fixture -session demo" \
  --output "$repo_dir/docs/traces.png" \
  --width 1100 \
  --padding 24 \
  --margin 16 \
  --window
