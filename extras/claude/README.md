# claude

Read a token parser review from a Claude transcript.

The demo replays an offline response fixture. It does not contact a model or claim a new agent run.

## Install

```sh
brew install roshbhatia/tap/traces-provider-claude
nix profile add 'github:roshbhatia/traces#provider-claude'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Read a token parser review from a Claude transcript](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/claude/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py claude` to record it.
