# Extras

Each provider directory owns its manifest, executable or script, and Nix
package definition. Harness readers emit the generic Traces newline-delimited
JSON protocol.

- `traces-provider-claude`
- `traces-provider-codex`
- `traces-provider-opencode`

`git` implements `diff.render` with a namespaced wrapper whose closure supplies
Git. `opencode` wraps its reader with the OpenCode CLI in `PATH`. The core and
the other provider closures do not inherit either dependency.

The flake discovers provider directories instead of listing their names. Each
provider remains a separate package, and CI validates it with only that package
and its manifest visible. Traces itself can run without any provider. Install
custom manifests in `~/.config/traces/providers`, or add their directory to
`TRACES_PROVIDER_PATH`.
