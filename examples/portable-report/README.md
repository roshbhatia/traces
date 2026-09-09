# Create a portable report

Keep collection, filtering, and presentation as separate pipeline stages.

```bash
traces-provider-codex --since 45m \
  | jq -c 'select(.session == "token-review")' \
  | traces --file - --session token-review --once --color never \
  > review.txt
```

The same newline-delimited stream can be archived, filtered, or sent to a
different renderer without coupling that renderer to Codex.

![Token parser activity](demo.gif)

[Tape source](demo.tape)

The recording uses an offline transcript fixture with output from real local regression tests.
Run `nix develop -c bash hack/recipe-demos.sh` to regenerate both recipe demos.
