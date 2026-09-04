{
  description = "Terminal agent trace viewer";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    systems.url = "github:nix-systems/default";
  };

  outputs =
    {
      self,
      nixpkgs,
      systems,
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
          version = "0.5.1";
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
              vendorHash = "sha256-DwUB0qEnJsjwfP9MWU56IdkHGI51jajzSzHLvrGXTK4=";
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
          traces = pkgs.buildGoModule {
            pname = "traces";
            inherit version;
            src = ./.;
            vendorHash = "sha256-DwUB0qEnJsjwfP9MWU56IdkHGI51jajzSzHLvrGXTK4=";
            subPackages = [ "." ];
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
              cue vet schema/provider.cue schema/check.cue
              for manifest in extras/*/provider.yaml; do
                cue vet schema/provider.cue "$manifest" -d '#Manifest'
              done
              if cue vet schema/provider.cue schema/fixtures/unsupported-action.yaml -d '#Manifest'; then
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
            paths = lib.attrValues providerPackages;
            meta = {
              description = "Optional providers for the Traces viewer";
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
        in
        {
          inherit traces extras full;
          default = traces;
        }
        // providerOutputs
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
            touch "$out"
          '';
          provider-neutral =
            pkgs.runCommand "traces-provider-neutral" { nativeBuildInputs = [ pkgs.ripgrep ]; }
              ''
                cd ${./.}
                ${pkgs.bash}/bin/bash ./hack/check-provider-neutral.sh
                touch "$out"
              '';
          schema-actions =
            pkgs.runCommand "traces-provider-schema-actions" { nativeBuildInputs = [ pkgs.cue ]; }
              ''
                cd ${./.}
                cue vet schema/provider.cue schema/check.cue
                if cue vet schema/provider.cue schema/fixtures/unsupported-action.yaml -d '#Manifest'; then
                  echo "unsupported provider action passed CUE validation" >&2
                  exit 1
                fi
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
            '';
          };
        }
      );
    };
}
