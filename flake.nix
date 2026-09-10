{
  description = "Terminal agent trace viewer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    systems.url = "github:nix-systems/default";
    # The canonical provider/v1 contract. schema/narrow.cue adds the Traces
    # action vocabulary on top of it; schema/provider.schema.json must stay
    # byte-identical to its export.
    provider-spec = {
      url = "github:roshbhatia/provider-spec/v1.0.0";
      flake = false;
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      systems,
      provider-spec,
      ...
    }:
    let
      supportedSystems = builtins.filter (system: system != "x86_64-darwin") (import systems);
      eachSystem = nixpkgs.lib.genAttrs supportedSystems;
      providerEntries = builtins.readDir ./extras;
      providerNames = builtins.filter (
        name:
        providerEntries.${name} == "directory"
        && builtins.pathExists (./extras + "/${name}/default.nix")
        && builtins.pathExists (./extras + "/${name}/provider.yaml")
      ) (builtins.attrNames providerEntries);
      # A tool is an extra with no manifest: a command shipped beside the
      # providers that answers no Traces action.
      toolNames = builtins.filter (
        name:
        providerEntries.${name} == "directory"
        && builtins.pathExists (./extras + "/${name}/default.nix")
        && !builtins.pathExists (./extras + "/${name}/provider.yaml")
      ) (builtins.attrNames providerEntries);
    in
    {
      formatter = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        pkgs.writeShellApplication {
          name = "traces-format";
          runtimeInputs = [
            pkgs.fd
            pkgs.nixfmt
          ];
          text = ''
            if [ "$#" -gt 0 ] && [ "''${1#-}" = "$1" ]; then
              exec nixfmt "$@"
            fi
            exec fd --extension nix --type file --exec-batch nixfmt "$@"
          '';
        }
      );

      packages = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          inherit (pkgs) lib;
          version = "0.11.2";
          providerMeta = name: {
            description = "Composable agent trace provider: ${name}";
            homepage = "https://github.com/roshbhatia/traces";
            license = lib.licenses.mit;
            mainProgram = "traces-provider-${name}";
            platforms = lib.platforms.unix;
          };
          mkGoProvider =
            {
              name,
              directory,
              runtimeInputs ? [ ],
            }:
            pkgs.buildGoModule {
              pname = "traces-provider-${name}";
              inherit version;
              src = ./.;
              vendorHash = "sha256-HY3tmW0rBCTf4CyVNItI39rrnJXwT23OdSv73jTLwH0=";
              subPackages = [ "./extras/${name}" ];
              nativeBuildInputs = lib.optionals (runtimeInputs != [ ]) [ pkgs.makeWrapper ];
              doCheck = false;
              postInstall = ''
                mv "$out/bin/${name}" "$out/bin/traces-provider-${name}"
                install -Dm644 ${directory}/provider.yaml \
                  "$out/share/traces/providers/${name}/provider.yaml"
              '';
              postFixup = lib.optionalString (runtimeInputs != [ ]) ''
                wrapProgram "$out/bin/traces-provider-${name}" \
                  --prefix PATH : ${lib.makeBinPath runtimeInputs}
              '';
              passthru.providerRuntimeInputs = runtimeInputs;
              meta = providerMeta name;
            };
          providerPackages = lib.genAttrs providerNames (
            name:
            import (./extras + "/${name}/default.nix") {
              inherit pkgs mkGoProvider;
            }
          );
          mkGoTool =
            { name, directory }:
            pkgs.buildGoModule {
              pname = "traces-${name}";
              inherit version;
              src = ./.;
              vendorHash = "sha256-HY3tmW0rBCTf4CyVNItI39rrnJXwT23OdSv73jTLwH0=";
              subPackages = [ "./extras/${name}" ];
              doCheck = false;
              postInstall = ''
                mv "$out/bin/${name}" "$out/bin/traces-${name}"
              '';
              meta = {
                description = "Composable agent trace tool: ${name}";
                homepage = "https://github.com/roshbhatia/traces";
                license = lib.licenses.mit;
                mainProgram = "traces-${name}";
                platforms = lib.platforms.unix;
              };
            };
          toolPackages = lib.genAttrs toolNames (
            name:
            import (./extras + "/${name}/default.nix") {
              inherit pkgs mkGoTool;
            }
          );
          traces = pkgs.buildGoModule {
            pname = "traces";
            inherit version;
            src = ./.;
            vendorHash = "sha256-HY3tmW0rBCTf4CyVNItI39rrnJXwT23OdSv73jTLwH0=";
            subPackages = [ "." ];
            ldflags = [ "-X main.version=${version}" ];
            nativeBuildInputs = [
              pkgs.cue
              pkgs.gitMinimal
              pkgs.installShellFiles
              pkgs.ripgrep
            ];
            doCheck = true;
            checkPhase = ''
              runHook preCheck
              go test -race ./...
              go run . generate --check
              cue vet ${provider-spec}/provider.cue schema/narrow.cue schema/check.cue
              for manifest in extras/*/provider.yaml; do
                cue vet -d '#Manifest' ${provider-spec}/provider.cue schema/narrow.cue "$manifest"
              done
              if cue vet -d '#Manifest' ${provider-spec}/provider.cue schema/narrow.cue schema/fixtures/unsupported-action.yaml; then
                echo "unsupported provider action passed CUE validation" >&2
                exit 1
              fi
              ${pkgs.bash}/bin/bash ./hack/check-provider-neutral.sh
              runHook postCheck
            '';
            postInstall = ''
              installShellCompletion \
                --cmd traces \
                --bash <("$out/bin/traces" completion bash) \
                --fish <("$out/bin/traces" completion fish) \
                --zsh <("$out/bin/traces" completion zsh)
              mkdir -p "$out/share/nushell/vendor/autoload"
              "$out/bin/traces" completion nu > "$out/share/nushell/vendor/autoload/traces.nu"
            '';
            meta = {
              description = "Composable agent trace viewer";
              homepage = "https://github.com/roshbhatia/traces";
              license = lib.licenses.mit;
              mainProgram = "traces";
              platforms = lib.platforms.unix;
            };
          };
          extras = pkgs.symlinkJoin {
            name = "traces-extras-${version}";
            paths = lib.attrValues providerPackages ++ lib.attrValues toolPackages;
            meta = {
              description = "Optional providers and tools for the Traces viewer";
              homepage = "https://github.com/roshbhatia/traces";
              license = lib.licenses.mit;
              platforms = lib.platforms.unix;
            };
          };
          full = pkgs.symlinkJoin {
            name = "traces-full-${version}";
            paths = [
              traces
              extras
            ];
            nativeBuildInputs = [ pkgs.makeWrapper ];
            postBuild = ''
              wrapProgram "$out/bin/traces" \
                --prefix XDG_DATA_DIRS : "$out/share" \
                --prefix PATH : "$out/bin"
            '';
            meta = traces.meta // {
              description = "Composable agent trace viewer with bundled providers";
            };
          };
          providerOutputs = lib.mapAttrs' (
            name: package: lib.nameValuePair "provider-${name}" package
          ) providerPackages;
          toolOutputs = lib.mapAttrs' (name: package: lib.nameValuePair "tool-${name}" package) toolPackages;
        in
        {
          inherit traces extras full;
          default = traces;
        }
        // providerOutputs
        // toolOutputs
      );

      apps = eachSystem (system: {
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.default}";
        };
        full = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.full}";
        };
      });

      checks = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          inherit (pkgs) lib;
          inherit (self.packages.${system}) extras full traces;
          closureOf = package: pkgs.closureInfo { rootPaths = [ package ]; };
          coreClosure = closureOf traces;
          extrasClosure = closureOf extras;
          fullClosure = closureOf full;
          providerChecks = lib.concatMapStringsSep "\n" (
            name:
            let
              package = self.packages.${system}.${"provider-${name}"};
            in
            ''
              test -x "${package}/bin/traces-provider-${name}"
              test ! -e "${package}/bin/${name}"
              test -f "${package}/share/traces/providers/${name}/provider.yaml"
              test -x "${extras}/bin/traces-provider-${name}"
              test -x "${full}/bin/traces-provider-${name}"
              case "$full_list" in
                *'"${name}"'*) ;;
                *) exit 1 ;;
              esac
              case "$full_names" in
                *${lib.escapeShellArg name}*) ;;
                *) exit 1 ;;
              esac
              isolated="$TMPDIR/provider-${name}"
              mkdir -p "$isolated/home" "$isolated/config" "$isolated/data"
              if ! ${pkgs.coreutils}/bin/env -i \
                HOME="$isolated/home" \
                XDG_CONFIG_HOME="$isolated/config" \
                XDG_DATA_HOME="$isolated/data" \
                XDG_DATA_DIRS="$isolated/data-dirs" \
                TRACES_PROVIDER_PATH="${package}/share/traces/providers" \
                PATH="${package}/bin:${pkgs.coreutils}/bin" \
                "${traces}/bin/traces" provider validate --json "${name}" \
                > "$isolated/validation.json"; then
                ${pkgs.coreutils}/bin/cat "$isolated/validation.json" >&2
                exit 1
              fi
              if ! ${pkgs.coreutils}/bin/env -i \
                HOME="$isolated/home" \
                XDG_CONFIG_HOME="$isolated/config" \
                XDG_DATA_HOME="$isolated/data" \
                XDG_DATA_DIRS="$isolated/data-dirs" \
                PATH="${pkgs.coreutils}/bin" \
                "${full}/bin/traces" provider validate --json "${name}" \
                > "$isolated/full-validation.json"; then
                ${pkgs.coreutils}/bin/cat "$isolated/full-validation.json" >&2
                exit 1
              fi
            ''
          ) providerNames;
          toolChecks = lib.concatMapStringsSep "\n" (
            name:
            let
              package = self.packages.${system}.${"tool-${name}"};
            in
            ''
              test -x "${package}/bin/traces-${name}"
              test ! -e "${package}/bin/${name}"
              test ! -e "${package}/share/traces/providers/${name}"
              test -x "${extras}/bin/traces-${name}"
              test -x "${full}/bin/traces-${name}"
            ''
          ) toolNames;
          providerClosureChecks = lib.concatMapStringsSep "\n" (
            name:
            let
              package = self.packages.${system}.${"provider-${name}"};
              packageClosure = closureOf package;
              otherNames = builtins.filter (other: other != name) providerNames;
              rejectOtherProviders = lib.concatMapStringsSep "\n" (
                other:
                let
                  otherPackage = self.packages.${system}.${"provider-${other}"};
                in
                ''! grep -Fqx "${otherPackage}" "${packageClosure}/store-paths"''
              ) otherNames;
            in
            ''
              grep -Fqx "${package}" "${packageClosure}/store-paths"
              ! grep -Fqx "${traces}" "${packageClosure}/store-paths"
              ${rejectOtherProviders}
            ''
          ) providerNames;
        in
        {
          default = self.packages.${system}.default;
          providers = pkgs.runCommand "traces-provider-layout" { } ''
            isolated="$TMPDIR/core"
            mkdir -p "$isolated/home" "$isolated/config" "$isolated/data"
            test ! -e "${extras}/bin/traces"
            test -x "${full}/bin/traces"
            result=$(${pkgs.coreutils}/bin/env -i \
              HOME="$isolated/home" \
              XDG_CONFIG_HOME="$isolated/config" \
              XDG_DATA_HOME="$isolated/data" \
              XDG_DATA_DIRS="$isolated/data-dirs" \
              PATH="${traces}/bin:${pkgs.coreutils}/bin" \
              "${traces}/bin/traces" provider list --json)
            test "$result" = '{}'
            full_list=$(${pkgs.coreutils}/bin/env -i \
              HOME="$isolated/home" \
              XDG_CONFIG_HOME="$isolated/config" \
              XDG_DATA_HOME="$isolated/data" \
              XDG_DATA_DIRS="$isolated/data-dirs" \
              PATH="${full}/bin:${pkgs.coreutils}/bin" \
              "${full}/bin/traces" provider list --json)
            full_names=$(${pkgs.coreutils}/bin/env -i \
              HOME="$isolated/home" \
              XDG_CONFIG_HOME="$isolated/config" \
              XDG_DATA_HOME="$isolated/data" \
              XDG_DATA_DIRS="$isolated/data-dirs" \
              PATH="${full}/bin:${pkgs.coreutils}/bin" \
              "${full}/bin/traces" provider list --names)
            ${providerChecks}
            ${toolChecks}
            touch "$out"
          '';
          provider-neutral =
            pkgs.runCommand "traces-provider-neutral" { nativeBuildInputs = [ pkgs.ripgrep ]; }
              ''
                cd ${./.}
                ${pkgs.bash}/bin/bash ./hack/check-provider-neutral.sh
                touch "$out"
              '';
          # The committed schema is the pinned spec export, every manifest
          # satisfies the spec plus schema/narrow.cue, and the binary reports
          # the spec version the flake pins.
          provider-spec-contract =
            pkgs.runCommand "traces-provider-spec-contract"
              {
                nativeBuildInputs = [
                  pkgs.cue
                  pkgs.diffutils
                ];
              }
              ''
                cd ${./.}
                export HOME="$TMPDIR"
                diff -u ${provider-spec}/schema/provider.schema.json schema/provider.schema.json
                cue vet ${provider-spec}/provider.cue schema/narrow.cue schema/check.cue
                for manifest in extras/*/provider.yaml; do
                  cue vet -d '#Manifest' ${provider-spec}/provider.cue schema/narrow.cue "$manifest"
                done
                for fixture in schema/fixtures/*.yaml; do
                  if cue vet -d '#Manifest' ${provider-spec}/provider.cue schema/narrow.cue "$fixture" 2>/dev/null; then
                    echo "reject expected: $fixture" >&2
                    exit 1
                  fi
                done
                ${traces}/bin/traces --version | grep --fixed-strings --line-regexp "provider/v1 spec $(cat ${provider-spec}/VERSION)"
                touch "$out"
              '';
          closures = pkgs.runCommand "traces-closure-boundaries" { nativeBuildInputs = [ pkgs.gnugrep ]; } ''
            grep -Fqx "${traces}" "${coreClosure}/store-paths"
            ! grep -Fqx "${traces}" "${extrasClosure}/store-paths"
            grep -Fqx "${traces}" "${fullClosure}/store-paths"
            ${lib.concatMapStringsSep "\n" (
              name:
              let
                package = self.packages.${system}.${"provider-${name}"};
              in
              ''
                ! grep -Fqx "${package}" "${coreClosure}/store-paths"
                grep -Fqx "${package}" "${extrasClosure}/store-paths"
                grep -Fqx "${package}" "${fullClosure}/store-paths"
              ''
            ) providerNames}
            ${providerClosureChecks}
            ${lib.concatMapStringsSep "\n" (
              name:
              let
                package = self.packages.${system}.${"tool-${name}"};
                packageClosure = closureOf package;
              in
              ''
                ! grep -Fqx "${package}" "${coreClosure}/store-paths"
                grep -Fqx "${package}" "${extrasClosure}/store-paths"
                ! grep -Fqx "${traces}" "${packageClosure}/store-paths"
              ''
            ) toolNames}
            touch "$out"
          '';
        }
      );

      devShells = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShellNoCC {
            packages = [
              pkgs.sqlite
              pkgs.python3
              pkgs.uv
              pkgs.ffmpeg
              pkgs.git
              pkgs.go
              pkgs.gopls
              pkgs.gotools
              pkgs.go-tools
              pkgs.goreleaser
              pkgs.jq
              pkgs.ripgrep
              pkgs.charm-freeze
              pkgs.vhs
              pkgs.fish
              pkgs.nushell
              pkgs.shfmt
              pkgs.cue
            ];
            shellHook = ''
              export GOTOOLCHAIN=local
              export PROVIDER_SPEC=${provider-spec}
            '';
          };
        }
      );
    };
}
