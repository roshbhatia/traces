#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  printf '%s\n' 'usage: traces-provider-git LOCAL REMOTE MERGED COLOR' >&2
  exit 2
fi

local_file=$1
remote_file=$2
merged_file=${3#./}
color=$4

set +e
output=$(git -c core.quotePath=false diff --no-index --color="$color" -- "$local_file" "$remote_file")
result=$?
set -e

if [[ $result -ne 0 && $result -ne 1 ]]; then
  exit "$result"
fi

local_label=${local_file#/}
remote_label=${remote_file#/}
merged_label=${merged_file#/}

replace_all() {
  local text=$1
  local needle=$2
  local replacement=$3
  local before after
  while [[ $text == *"$needle"* ]]; do
    before=${text%%"$needle"*}
    after=${text#*"$needle"}
    text=$before$replacement$after
  done
  REPLY=$text
}

replace_all "$output" "a/$local_label" "a/$merged_label"
output=$REPLY
replace_all "$output" "b/$remote_label" "b/$merged_label"
output=$REPLY
printf '%s\n' "$output"
exit "$result"
