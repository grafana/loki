{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    flake-utils.inputs.nixpkgs.follows = "nixpkgs";

    rnix-parser.url = "github:nix-community/rnix-parser";
    rnix-parser.inputs.nixpkgs.follows = "nixpkgs";

    # helm-docs @ 1.8.1
    helm-docs-nixpkgs.url = "nixpkgs/bf972dc380f36a3bf83db052380e55f0eaa7dcb6";
  };

  # Nixpkgs / NixOS version to use.

  outputs = { self, nixpkgs, flake-utils, rnix-parser, helm-docs-nixpkgs }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        golangci-lint-overlay = final: prev: {
          golangci-lint = prev.callPackage
            "${prev.path}/pkgs/development/tools/golangci-lint"
            {
              buildGoModule = args:
                prev.buildGoModule (args // rec {
                  version = "1.45.2";

                  src = prev.fetchFromGitHub rec {
                    owner = "golangci";
                    repo = "golangci-lint";
                    rev = "v${version}";
                    sha256 =
                      "sha256-Mr45nJbpyzxo0ZPwx22JW2WrjyjI9FPpl+gZ7NIc6WQ=";
                  };

                  vendorSha256 =
                    "sha256-pcbKg1ePN8pObS9EzP3QYjtaty27L9sroKUs/qEPtJo=";
                });
            };
        };

        nix = import ./nix { inherit self nixpkgs system; };

        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            rnix-parser.overlay
            golangci-lint-overlay
            nix.overlay
            (final: prev: {
              inherit (import helm-docs-nixpkgs { inherit system; }) helm-docs;
            })
          ];
          config = { allowUnfree = true; };
        };
      in
      with pkgs; {
        # The default package for 'nix build'. This makes sense if the
        # flake provides only one package or there is a clear "main"
        # package.
        defaultPackage = loki;

        packages = { inherit loki; };

        checks = {
          format = runCommand "check-format" {
            buildInputs = [ nixfmt ];
          } ''nixfmt'';
        };

        devShell = pkgs.mkShell {
          nativeBuildInputs = [
            gcc
            go
            systemd
            yamllint
            nixfmt
            /* rnix-parser */
            nettools

            golangci-lint
            helm-docs
            faillint
          ];

          shellHook = ''
            pushd $(git rev-parse --show-toplevel) > /dev/null || exit 1
            ./nix/generate-build-vars.sh
            popd > /dev/null || exit 1
          '';
        };
      });
}
