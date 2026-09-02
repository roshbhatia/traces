# Extras

Each command reads one harness-specific activity store and emits the generic
Traces newline-delimited JSON protocol.

- `traces-provider-claude`
- `traces-provider-codex`
- `traces-provider-opencode`

They remain separate packages. Traces itself can run without any harness
reader and can use replacement commands through YAML provider manifests.
