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
          version = "0.4.0";
          mkPackage =
            {
              name,
              subPackage,
              completions ? false,
            }:
            pkgs.buildGoModule {
              pname = name;
              inherit version;
              src = ./.;
              vendorHash = "sha256-SY6o7kaHBn0k221+q/GdsYa+iJ/NIldkl9y+Klh4Ztc=";
              subPackages = [ subPackage ];
              nativeBuildInputs = pkgs.lib.optionals completions [
                pkgs.gitMinimal
                pkgs.installShellFiles
              ];
              doCheck = completions;
              checkPhase = pkgs.lib.optionalString completions ''
                runHook preCheck
                go test -race ./...
                go run . generate --check
                runHook postCheck
              '';
              postInstall = pkgs.lib.optionalString completions ''
                installShellCompletion \
                  --cmd traces \
                  --bash <("$out/bin/traces" completion bash) \
                  --fish <("$out/bin/traces" completion fish) \
                  --zsh <("$out/bin/traces" completion zsh)
                mkdir -p "$out/share/nushell/vendor/autoload"
                "$out/bin/traces" completion nu > "$out/share/nushell/vendor/autoload/traces.nu"
              '';
              meta = {
                description = "Composable agent trace viewer component";
                homepage = "https://github.com/roshbhatia/traces";
                license = pkgs.lib.licenses.mit;
                mainProgram = name;
                platforms = pkgs.lib.platforms.unix;
              };
            };
          traces = mkPackage {
            name = "traces";
            subPackage = ".";
            completions = true;
          };
          claude = mkPackage {
            name = "traces-provider-claude";
            subPackage = "./extras/traces-provider-claude";
          };
          codex = mkPackage {
            name = "traces-provider-codex";
            subPackage = "./extras/traces-provider-codex";
          };
          opencode = mkPackage {
            name = "traces-provider-opencode";
            subPackage = "./extras/traces-provider-opencode";
          };
          full = pkgs.symlinkJoin {
            name = "traces-full-${version}";
            paths = [
              traces
              claude
              codex
              opencode
            ];
          };
        in
        {
          inherit traces full;
          provider-claude = claude;
          provider-codex = codex;
          provider-opencode = opencode;
          default = traces;
        }
      );

      apps = eachSystem (system: {
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe self.packages.${system}.default}";
        };
      });

      checks = eachSystem (system: {
        default = self.packages.${system}.default;
      });

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
              pkgs.ripgrep
              pkgs.charm-freeze
              pkgs.vhs
              pkgs.fish
              pkgs.nushell
              pkgs.shfmt
            ];
            shellHook = ''
              export GOTOOLCHAIN=local
            '';
          };
        }
      );
    };
}
