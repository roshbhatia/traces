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
      eachSystem = nixpkgs.lib.genAttrs (import systems);
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
              vendorHash = "sha256-L8QNZBHgLDaFuy7QrML8PHtiAkeFAJBtVxsNFTIHsnk=";
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
          mkShellProvider =
            {
              name,
              directory,
              runtimeInputs ? [ ],
            }:
            pkgs.stdenvNoCC.mkDerivation {
              pname = "traces-provider-${name}";
              inherit version;
              dontUnpack = true;
              nativeBuildInputs = [ pkgs.makeWrapper ];
              installPhase = ''
                runHook preInstall
                install -Dm755 ${directory}/main.sh "$out/bin/traces-provider-${name}"
                patchShebangs "$out/bin/traces-provider-${name}"
                install -Dm644 ${directory}/provider.yaml \
                  "$out/share/traces/providers/${name}/provider.yaml"
                wrapProgram "$out/bin/traces-provider-${name}" \
                  --prefix PATH : ${lib.makeBinPath runtimeInputs}
                runHook postInstall
              '';
              passthru.providerRuntimeInputs = runtimeInputs;
              meta = providerMeta name;
            };
          providerPackages = lib.genAttrs providerNames (
            name:
            import (./extras + "/${name}/default.nix") {
              inherit pkgs mkGoProvider mkShellProvider;
            }
          );
          traces = pkgs.buildGoModule {
            pname = "traces";
            inherit version;
            src = ./.;
            vendorHash = "sha256-L8QNZBHgLDaFuy7QrML8PHtiAkeFAJBtVxsNFTIHsnk=";
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
              ./hack/check-provider-neutral.sh
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
          full = pkgs.symlinkJoin {
            name = "traces-full-${version}";
            paths = [ traces ] ++ lib.attrValues providerPackages;
          };
          providerOutputs = lib.mapAttrs' (
            name: package: lib.nameValuePair "provider-${name}" package
          ) providerPackages;
        in
        {
          inherit traces full;
          default = traces;
        }
        // providerOutputs
      );

      apps = eachSystem (system: {
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.default}";
        };
      });

      checks = eachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          inherit (pkgs) lib;
          inherit (self.packages.${system}) full traces;
          providerChecks = lib.concatMapStringsSep "\n" (
            name:
            let
              package = self.packages.${system}.${"provider-${name}"};
              providerPath = lib.makeBinPath (package.providerRuntimeInputs or [ ]);
              providerPathPrefix = lib.optionalString (providerPath != "") "${providerPath}:";
            in
            ''
              test -x "${package}/bin/traces-provider-${name}"
              test ! -e "${package}/bin/${name}"
              test -f "${package}/share/traces/providers/${name}/provider.yaml"
              test -x "${full}/bin/traces-provider-${name}"
              case "$full_list" in
                *'"${name}"'*) ;;
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
                PATH="${package}/bin:${providerPathPrefix}${pkgs.coreutils}/bin" \
                "${traces}/bin/traces" provider validate --json "${name}" \
                > "$isolated/validation.json"; then
                ${pkgs.coreutils}/bin/cat "$isolated/validation.json" >&2
                exit 1
              fi
            ''
          ) providerNames;
        in
        {
          default = self.packages.${system}.default;
          providers = pkgs.runCommand "traces-provider-layout" { } ''
            isolated="$TMPDIR/core"
            mkdir -p "$isolated/home" "$isolated/config" "$isolated/data"
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
            ${providerChecks}
            touch "$out"
          '';
          provider-neutral =
            pkgs.runCommand "traces-provider-neutral" { nativeBuildInputs = [ pkgs.ripgrep ]; }
              ''
                cd ${./.}
                ./hack/check-provider-neutral.sh
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
