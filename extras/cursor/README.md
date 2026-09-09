# cursor

Replay an offline token parser review with cursor.

The demo replays an offline response fixture. It does not contact a model or claim a new agent run.

## Install

```sh
brew install roshbhatia/tap/traces-provider-cursor
nix profile add 'github:roshbhatia/traces#provider-cursor'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Replay an offline token parser review with cursor](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/cursor/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py cursor` to record it.
