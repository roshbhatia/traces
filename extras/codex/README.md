# codex

Read a token parser repair from a Codex transcript.

The demo replays an offline response fixture. It does not contact a model or claim a new agent run.

## Install

```sh
brew install roshbhatia/tap/traces-provider-codex
nix profile add 'github:roshbhatia/traces#provider-codex'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Read a token parser repair from a Codex transcript](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/codex/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py codex` to record it.
