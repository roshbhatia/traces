# traces

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

## Development

```bash
nix develop
go test -race ./...
nix flake check
```
