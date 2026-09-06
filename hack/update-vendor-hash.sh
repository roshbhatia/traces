#!/usr/bin/env bash
set -euo pipefail

flake_file="flake.nix"
fake_hash="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
backup=$(mktemp)
build_log=$(mktemp)
complete=false

cleanup() {
  if [[ $complete != true ]]; then
    cp "$backup" "$flake_file"
  fi
  rm -f "$backup" "$build_log"
}
trap cleanup EXIT

cp "$flake_file" "$backup"
sed "s|vendorHash = \"sha256-[^\"]*\";|vendorHash = \"$fake_hash\";|" \
  "$backup" >"$flake_file"

if nix build .#traces --no-link >"$build_log" 2>&1; then
  echo "The fake vendor hash unexpectedly built successfully." >&2
  exit 1
fi

vendor_hash=$(sed -nE 's/^[[:space:]]*got:[[:space:]]*(sha256-[A-Za-z0-9+\/=]+).*$/\1/p' \
  "$build_log" | head -1)
if [[ -z $vendor_hash ]]; then
  cat "$build_log" >&2
  echo "Could not determine the Go vendor hash." >&2
  exit 1
fi

sed "s|vendorHash = \"sha256-[^\"]*\";|vendorHash = \"$vendor_hash\";|" \
  "$backup" >"$flake_file"
complete=true
echo "Updated vendorHash to $vendor_hash"
