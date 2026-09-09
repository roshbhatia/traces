# opencode

Read an offline token parser review export.

The demo replays an offline response fixture. It does not contact a model or claim a new agent run.

## Install

```sh
brew install roshbhatia/tap/traces-provider-opencode
nix profile add 'github:roshbhatia/traces#provider-opencode'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Read an offline token parser review export](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/opencode/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py opencode` to record it.
