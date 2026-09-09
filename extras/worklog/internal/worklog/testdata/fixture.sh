#!/usr/bin/env bash
# Builds the session the golden records describe under $1: one repository a
# commit ahead of its upstream with an uncommitted edit, two more of the same
# shape under a sessions root, and one Claude Code transcript. Every commit
# carries a fixed date, so the short hashes in the golden are stable.
set -euo pipefail

root=$1
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=Probe GIT_AUTHOR_EMAIL=probe@example.com
export GIT_COMMITTER_NAME=Probe GIT_COMMITTER_EMAIL=probe@example.com
export GIT_AUTHOR_DATE=2026-08-13T18:00:00Z GIT_COMMITTER_DATE=2026-08-13T18:00:00Z

repo() {
  local upstream=$1 work=$2
  git init --quiet --bare --initial-branch=main "$upstream"
  git clone --quiet "$upstream" "$work" 2> /dev/null
  printf 'one\n' > "$work/base.txt"
  git -C "$work" add -A
  git -C "$work" commit --quiet -m 'base commit'
  git -C "$work" push --quiet origin main
  git -C "$work" remote set-head origin main
  printf 'two\nthree\n' > "$work/added.txt"
  git -C "$work" add -A
  git -C "$work" commit --quiet -m 'add a file'
  printf 'one\nedited\n' > "$work/base.txt"
}

repo "$root/upstream.git" "$root/work"
mkdir -p "$root/sessions/a-session"
repo "$root/alpha.git" "$root/sessions/a-session/alpha"
repo "$root/zeta.git" "$root/sessions/a-session/zeta"
mkdir -p "$root/sessions/a-session/notes"
printf 'x\n' > "$root/sessions/a-session/notes/todo.md"

mkdir -p "$root/transcripts"
cat > "$root/transcripts/s1.jsonl" << 'JSONL'
{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"s1","timestamp":"2026-08-13T18:34:00.512Z","cwd":"/work","gitBranch":"main","message":{"role":"user","content":"first   prompt\nwith breaks"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","requestId":"req_1","timestamp":"2026-08-13T18:35:00.000Z","message":{"model":"claude-opus-5","role":"assistant","content":[{"type":"text","text":"working"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-08-13T18:35:10.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"base.txt"}]},"toolUseResult":{"stdout":"base.txt","stderr":""}}
{"type":"user","uuid":"u3","parentUuid":"u2","sessionId":"s1","timestamp":"2026-08-13T18:40:00.000Z","message":{"role":"user","content":[{"type":"text","text":"second prompt"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u3","sessionId":"s1","requestId":"req_2","timestamp":"2026-08-13T18:41:00.000Z","message":{"model":"claude-sonnet-5","role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}}
JSONL
