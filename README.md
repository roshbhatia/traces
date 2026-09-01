# traces

`traces` renders local agent activity as a folding trace tree. It reads OTLP,
Codex rollout files, OpenCode events, and harness transcripts.

The interactive view supports filtering, session selection, inspection, and
multi-repository change review.

## Development

```bash
nix develop
go test -race ./...
nix flake check
```
