# Combine local harness activity

Install only the providers used on the machine, then map harness services to
those names in `~/.config/traces/config.yaml`.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/roshbhatia/traces/main/schema/traces.schema.json
providers:
  codex:
    command: [traces-provider-codex]
    capabilities: [activity]
sources:
  codex: [codex]
  codex_cli_rs: [codex]
```

`traces --service codex` now starts only the declared Codex provider. OTLP from
the collector remains available at the same time.
