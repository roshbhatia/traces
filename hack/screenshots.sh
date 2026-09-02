#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$repo_dir/docs"
build_dir=$(mktemp -d)
fixture=$(mktemp)
trap 'rm -rf "$build_dir" "$fixture"' EXIT

go build -o "$build_dir/traces" .
printf '%s\n' \
  '{"traceId":"demo","spanId":"turn","name":"agent.turn","service":"codex","session":"demo","startUnixNano":"1788278400000000000","endUnixNano":"1788278412000000000","attrs":{"prompt":"Move diff rendering behind a provider contract and prove the fallback still works","cwd":"/work/changes"}}' \
  '{"traceId":"demo","spanId":"plan","parentId":"turn","name":"agent.model","service":"codex","session":"demo","startUnixNano":"1788278400500000000","endUnixNano":"1788278401800000000","attrs":{"model":"gpt-5.6-sol","stop_reason":"tool_use","ttft_ms":"420","duration_ms":"1300"}}' \
  '{"traceId":"demo","spanId":"search","parentId":"turn","name":"agent.tool","service":"codex","session":"demo","startUnixNano":"1788278401900000000","endUnixNano":"1788278402400000000","attrs":{"tool_name":"Shell","traces.action":"search","input":"rg -n \\"renderPatch|DiffProvider\\" internal"}}' \
  '{"traceId":"demo","spanId":"edit","parentId":"turn","name":"agent.edit","service":"codex","session":"demo","startUnixNano":"1788278402500000000","endUnixNano":"1788278406100000000","attrs":{"tool_name":"Edit","traces.action":"edit","traces.patch":"diff --git a/internal/ui/diff.go b/internal/ui/diff.go\\n@@ -18 +18 @@\\n-return renderBuiltin(patch)\\n+return provider.Render(patch)"}}' \
  '{"traceId":"demo","spanId":"test","parentId":"turn","name":"agent.tool","service":"codex","session":"demo","startUnixNano":"1788278406200000000","endUnixNano":"1788278409800000000","attrs":{"tool_name":"Shell","traces.action":"test","input":"go test -race ./...","output":"ok  github.com/roshbhatia/changes/internal/ui"}}' \
  '{"traceId":"demo","spanId":"review","parentId":"turn","name":"agent.model","service":"codex","session":"demo","startUnixNano":"1788278410000000000","endUnixNano":"1788278411800000000","attrs":{"model":"gpt-5.6-sol","stop_reason":"end_turn","ttft_ms":"510","duration_ms":"1800"}}' \
  > "$fixture"

PATH="$build_dir:$PATH" freeze \
  --execute "traces -once -color always -provider , -file $fixture -session demo" \
  --output "$repo_dir/docs/traces.png" \
  --width 1100 \
  --padding 24 \
  --margin 16 \
  --window
