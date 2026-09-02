# Combine local harness activity

Install only the providers used on the machine, then map harness services to
those names in `~/.config/traces/config.yaml`. The adjacent
[`config.yaml`](config.yaml) contains the complete local example.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/roshbhatia/traces/main/schema/traces.schema.json
providers:
  codex:
    description: Read Codex rollout activity
    command: [traces-provider-codex]
    capabilities: [activity]
sources:
  codex: [codex]
  codex_cli_rs: [codex]
```

`traces --service codex` now starts only the declared Codex provider. OTLP from
the collector remains available at the same time.

Validate the configured commands and protocol output before opening the TUI:

```bash
TRACES_CONFIG="$PWD/examples/local-harnesses/config.yaml" traces provider validate
```
