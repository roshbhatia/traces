# traces

![Traces tree view](docs/traces.png)

![Traces interactive agent run](docs/traces.gif)

![Traces non-interactive report](docs/traces-noninteractive.gif)

`traces` renders agent activity as a folding trace tree. The core reads OTLP
and a newline-delimited provider protocol. It does not import a harness,
observability service, difftool, terminal multiplexer, or terminal emulator.

The interactive view supports filtering, session selection, inspection, and
multi-repository change review. The key help, leader bar, and footer render
from one binding registry.

## Use it

Install only the provider-neutral core:

```bash
nix profile install github:roshbhatia/traces
```

Install every bundled provider beside the core, or select only the extras you
use. The `full` package combines the core with every bundled provider.
Nix packages support Apple Silicon macOS and ARM or x86-64 Linux.

```bash
nix profile install github:roshbhatia/traces github:roshbhatia/traces#extras
nix profile install github:roshbhatia/traces#full
nix profile install github:roshbhatia/traces#provider-codex
nix profile install github:roshbhatia/traces#provider-git
```

Install the provider-neutral core and its shell completions with Homebrew:

```bash
brew install roshbhatia/tap/traces
```

`go install github.com/roshbhatia/traces@latest` also installs only the core.
Install providers separately or write a provider manifest for the external
commands you use.

```bash
# Attach to activity for the current directory.
traces

# Produce a stable report for automation.
traces --once --session 01abc --color never

# Keep the protocol composable in a pipeline.
traces-provider-codex --since 30m | traces --file - --once
```

See [`examples/local-harnesses`](examples/local-harnesses/README.md) and
[`examples/portable-report`](examples/portable-report/README.md) for complete
workflows.

## Configure it

Traces loads `~/.config/traces/config.yaml`. Set `TRACES_CONFIG` to select
another file. Nested environment names override YAML, such as
`TRACES_COLOR=never`, `TRACES_PROVIDERS_DIRECTORY`, or
`TRACES_DIFF_PROVIDER=git`. Use `TRACES_CLIPBOARD_PROVIDER` and
`TRACES_EDITOR_PROVIDER` to select optional host-action providers.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/roshbhatia/traces/main/schema/traces.schema.json
color: auto
sources:
  claude-code: [claude]
  codex: [codex]
  codex_cli_rs: [codex]
  opencode: [opencode]
diff:
  provider: git
clipboard:
  provider: desktop
editor:
  provider: desktop
```

Provider manifests use the shared `provider/v1` contract. Traces recognizes
seven capabilities: `activity.read`, `session.current`, `session.discover`,
`diff.render`, `clipboard.write`, `document.open`, and `provider.validate`.
Each action defines direct argv and environment Go templates.
Traces never inserts a shell. An activity provider writes newline-delimited
spans and events to standard output. Several providers may serve one harness.
Traces merges their output and removes duplicate spans.

Activity providers normalize native events to `agent.turn`, `agent.model`,
`agent.tool`, or `agent.edit`. A tool span sets `traces.action` when its native
name maps to a generic action such as `shell`, `search`, `edit`, or `delegate`.
The core uses the native name as a fallback and contains no per-provider map.

The `diff.render` action receives `.Local`, `.Remote`, `.Merged`, `.Width`, and
`.Color` template data. Without a diff provider, Traces uses its built-in
renderer. Rendered diffs are cached by provider manifest, width, and patch.
The provider executable and interpreter scripts are part of the cache identity.
Traces expires cached diffs after seven days and keeps at most 128 entries.

`clipboard.write` and `document.open` receive `.Path`, which points to a
temporary file. Traces performs no host action when its provider is absent.
The bundled `desktop` provider selects the host command. The `full` package
includes it, but it remains disabled until configuration selects it.

A manifest with `requires` or a side-effect capability must implement
`provider.validate`. That action checks dependencies from inside the provider
runtime and returns JSON checks for every declared command, environment name,
path, and side-effect capability. Traces runs it in an isolated environment.
It renders clipboard and document actions during validation but never performs
them.

Traces discovers manifests in this order. The first manifest for a name wins:

1. `providers.directory` in the Traces configuration. It defaults to
   `~/.config/traces/providers`.
2. Each directory in `TRACES_PROVIDER_PATH`.
3. `share/traces/providers` beside a flat release executable.
4. `../share/traces/providers` beside an installed `bin/traces`.
5. `$XDG_DATA_HOME/traces/providers`.
6. Each `$XDG_DATA_DIRS` entry under `traces/providers`.

Harness readers live in `extras/` as separate binaries. Private providers can
stay in downstream configuration without changing or rebuilding Traces. Use
`providers/<name>/provider.yaml` for the installed manifest layout. A bundled
provider directory also supplies `default.nix` and its command source. This
keeps its runtime dependencies inside that provider package.

Inspect and test providers without opening the TUI:

```bash
traces provider list
traces provider validate
traces provider validate codex
```

Validation checks the manifest, templates, provider-owned dependencies, and
executable. It probes every safe action in isolation and fails if any discovered
manifest is invalid. Normal interactive discovery reports and skips one invalid
optional provider so unrelated sources remain available.

Tagged releases publish separate core, full, and per-provider archives. Core
and full archives include Bash, Zsh, Fish, and Nushell completions, README, and
LICENSE. The full archive's `traces` launcher resolves its bundled manifests,
core, and provider binaries relative to itself, so it does not depend on the
caller's `PATH`. Release archives also include x86-64 macOS binaries for users
outside the supported Nix systems.

Generate the schema and command reference with `traces generate`. CI uses
`traces generate --check` to reject stale output.

## Command reference
<!-- BEGIN GENERATED:cli -->

### `traces`

Inspect agent activity as a trace tree

| Option | Description |
| --- | --- |
| `--all` | Show every local run |
| `--color` `<value>` | Color output |
| `--config` `<value>` | YAML configuration file |
| `--file` `<value>` | Read an OTLP JSON file |
| `--json` | Print newline-delimited JSON |
| `--lag` `<value>` | Provider overlap window |
| `--list` | List sessions |
| `--once` | Print one trace tree |
| `--poll` `<value>` | Provider poll interval |
| `--provider` `<value>` | Read named activity providers |
| `--service` `<value>` | Filter by service |
| `--session` `<value>` | Attach by session ID or prefix |
| `--since` `<value>` | Initial provider window |

### `traces generate`

Generate README command docs and JSON Schema

| Option | Description |
| --- | --- |
| `--check` | Fail when generated files are stale |

### `traces provider`

Inspect and validate external providers

### `traces provider list`

List discovered providers

| Option | Description |
| --- | --- |
| `--config` `<value>` | YAML configuration file |
| `--json` | Print JSON |
| `--names` | Print provider names, one per line |

### `traces provider validate`

Validate provider commands and protocol output

| Option | Description |
| --- | --- |
| `--config` `<value>` | YAML configuration file |
| `--json` | Print JSON |

<!-- END GENERATED:cli -->

## Development

```bash
nix develop
go test -race ./...
go run . generate --check
./hack/check-provider-neutral.sh
nix flake check
./hack/screenshots.sh
```
