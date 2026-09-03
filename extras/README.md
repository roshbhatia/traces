# Extras

Each directory contains a provider/v1 manifest beside its executable. Harness
readers emit the generic Traces newline-delimited JSON protocol.

- `traces-provider-claude`
- `traces-provider-codex`
- `traces-provider-opencode`

`git/provider.yaml` is a manifest-only example for `diff.render`.

They remain separate packages. Traces itself can run without any harness
reader. Install custom manifests in `~/.config/traces/providers`, or add their
directory to `TRACES_PROVIDER_PATH`.
