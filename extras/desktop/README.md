# desktop

Open the saved token parser review report.

## Install

```sh
brew install roshbhatia/tap/traces-provider-desktop
nix profile add 'github:roshbhatia/traces#provider-desktop'
```

Install the core utility separately, or select its all-provider bundle. Runtime tools still need their own credentials.

## Demo

![Open the saved token parser review report](demo.gif)

[Tape source](demo.tape) · [Task script](demo.sh)

Run `nix develop -c bash extras/desktop/demo.sh` to run the task without recording.
Run `nix develop -c python3 hack/extra-demos.py desktop` to record it.
