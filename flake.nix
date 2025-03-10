{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs
          {
            inherit system;
            config = { allowUnfree = true; };
          };
      in
      rec {
        defaultPackage = pkgs.loki;

        packages = import ./nix {
          inherit self pkgs;
          inherit (pkgs) lib;
        };

        apps = {
          lint = {
            type = "app";
            program = with pkgs; "${
                (writeShellScriptBin "lint.sh" ''
                  ${nixpkgs-fmt}/bin/nixpkgs-fmt --check ${self}/flake.nix ${self}/nix/*.nix
                  ${statix}/bin/statix check ${self}
                '')
              }/bin/lint.sh";
          };
        };

        devShell = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            (pkgs.callPackage ./nix/packages/chart-releaser.nix {
              inherit pkgs;
              inherit (pkgs) buildGoModule fetchFromGitHub;
            })

            chart-testing
            gcc
            go
            golangci-lint
            gotools
            helm-docs
            nettools
            nixpkgs-fmt
            statix
            yamllint
          ] // packages;
        };
      });
}
