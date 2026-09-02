# traces

![Traces tree view](docs/traces.png)

`traces` renders local agent activity as a folding trace tree. It reads OTLP,
Codex rollout files, OpenCode events, and harness transcripts.

The interactive view supports filtering, session selection, inspection, and
multi-repository change review.

External sources use the same Unix command pattern as the built-in sources. A
provider named `observe` resolves to `traces-observe` on `PATH`. It receives a
`--since` window and an optional `--session`. It writes newline-delimited spans
and events to standard output.

Several provider names can serve one harness. Traces merges their output and
removes duplicate spans. Machine-specific providers can stay in downstream
configuration. This keeps private telemetry queries outside this repository.

The key help, leader bar, and footer render from one binding registry.

Set `TRACES_DIFF_PROVIDER=changes` to render change panes through Changes. The
provider receives a patch on standard input through its `render` subcommand.
Traces caches each rendered patch by provider, width, and content.

Generate shell completions with `traces completion bash`, `zsh`, `fish`, or
`nu`.

## Development

```bash
nix develop
go test -race ./...
nix flake check
./hack/screenshots.sh
```
