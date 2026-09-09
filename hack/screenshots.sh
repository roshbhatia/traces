#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
recording=$(mktemp -d /tmp/traces-ci-recording.XXXXXX)
printf 'Recording inputs and output: %s\n' "$recording" >&2

cp "$repo_dir/hack/recordings/orc-ci.json" "$recording/run.json"
jq -c -f "$repo_dir/hack/github-run.jq" "$recording/run.json" > "$recording/orc-ci.jsonl"
go build -o "$recording/traces" "$repo_dir"
cd "$recording"
export PATH="$recording:$PATH"
traces -once -all -provider , -file orc-ci.jsonl > report.txt
vhs "$repo_dir/hack/traces.tape" --output traces.gif
ffmpeg -v error -y -ss 25 -i traces.gif -frames:v 1 traces.png
install -m 0644 traces.gif "$repo_dir/docs/traces.gif"
install -m 0644 traces.png "$repo_dir/docs/traces.png"
