# Create a portable report

Keep collection, filtering, and presentation as separate pipeline stages.

```bash
traces-provider-codex --since 45m \
  | jq -c 'select(.attrs.cwd | startswith("/work/payments"))' \
  | traces --file - --once --color never \
  > review.txt
```

The same newline-delimited stream can be archived, filtered, or sent to a
different renderer without coupling that renderer to Codex.
