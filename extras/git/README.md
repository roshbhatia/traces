# git

Inspect the token parser patch.

## Install

```sh
brew install roshbhatia/tap/traces-provider-git
nix profile add 'github:roshbhatia/traces#provider-git'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Inspect the token parser patch](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/git/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py git` to record it.
