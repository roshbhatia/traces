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
          traces = pkgs.buildGoModule {
            pname = "traces";
            version = "0.2.0";
            src = ./.;
            vendorHash = "sha256-aWLUPzUTpAgFlnTxBXfmwEAE1SAMjk+Kj7XrcpLLKs8=";
            nativeBuildInputs = [ pkgs.installShellFiles ];
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
              description = "Inspect local agent activity as a folding trace tree";
              homepage = "https://github.com/roshbhatia/traces";
              license = pkgs.lib.licenses.mit;
              mainProgram = "traces";
              platforms = pkgs.lib.platforms.unix;
            };
          };
        in
        {
          inherit traces;
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
