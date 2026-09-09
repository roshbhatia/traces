# Extras

Each provider directory owns its manifest, executable or script, and Nix
package definition. Harness readers emit the generic Traces newline-delimited
JSON protocol.

`git` implements `diff.render` with a namespaced wrapper whose closure supplies
Git. `opencode` wraps its reader with the OpenCode CLI in `PATH`. The core and
the other provider closures do not inherit either dependency. `desktop`
implements the optional `clipboard.write` and `document.open` host actions.

A reader two extras need lives under `extras/internal/`, which Go opens to
every directory below `extras/` and to nothing above it. The Claude Code
transcript reader is there, at `extras/internal/claude/transcript`.

`gate` reads the hook dispatcher's decision log and keys each verdict into the
harness session it interrupted, so a denied call is a row beside the calls that
ran. List it under that harness's service name in `sources`.

A directory with a `default.nix` and no manifest is a tool: a command shipped
beside the providers that answers no Traces action. There is one.

- `traces-worklog` reduces one finished session to one JSON line for a report
  over many sessions: the repositories it moved and by how much, the first and
  last prompt, the model, and the duration. A harness's session-end hook runs
  it with the harness's payload on stdin. It reads the transcript through the
  shared Claude Code reader and parses none of its own. The line is schema v2,
  and a golden test holds a record the previous writer produced.

The flake discovers provider directories instead of listing their names. Each
provider remains a separate package, and CI validates it with only that package
and its manifest visible. Traces itself can run without any provider. Install
custom manifests in `~/.config/traces/providers`, or add their directory to
`TRACES_PROVIDER_PATH`.

Install the provider-only bundle with `github:roshbhatia/traces#extras`. It
carries the tools too. Install one tool with its `tool-<name>` package, such
as `github:roshbhatia/traces#tool-worklog`.
Install one provider with its `provider-<name>` package, such as
`github:roshbhatia/traces#provider-git`. Install `#full` for the core and all
bundled providers. The default package remains the provider-neutral core.
Installing `desktop` does not enable it; select it through
`clipboard.provider` and `editor.provider`.

<!-- BEGIN GENERATED CATALOG -->

| Extra | Task | Demo |
|---|---|---|
| [antigravity](antigravity/README.md) | Replay an offline token parser review with antigravity | [Tape](antigravity/demo.tape) |
| [claude](claude/README.md) | Read a token parser review from a Claude transcript | [Tape](claude/demo.tape) |
| [codex](codex/README.md) | Read a token parser repair from a Codex transcript | [Tape](codex/demo.tape) |
| [cursor](cursor/README.md) | Replay an offline token parser review with cursor | [Tape](cursor/demo.tape) |
| [desktop](desktop/README.md) | Open the saved token parser review report | [Tape](desktop/demo.tape) |
| [devin](devin/README.md) | Replay an offline token parser review with devin | [Tape](devin/demo.tape) |
| [gate](gate/README.md) | Replay an offline token parser review with gate | [Tape](gate/demo.tape) |
| [git](git/README.md) | Inspect the token parser patch | [Tape](git/demo.tape) |
| [opencode](opencode/README.md) | Read an offline token parser review export | [Tape](opencode/demo.tape) |
| [worklog](worklog/README.md) | Replay an offline token parser review with worklog | [Tape](worklog/demo.tape) |

<!-- END GENERATED CATALOG -->
