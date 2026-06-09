{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-25.11";
    nixpkgs-unstable.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { self
    , nixpkgs
    , nixpkgs-unstable
    , flake-utils
    ,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        # The go version in nixpkgs often trails behind the latest by a few
        # days. Overlay to pin to the exact version go.mod expects.
        goOverlay = final: prev: {
          go_1_26 = prev.go_1_26.overrideAttrs (old: rec {
            version = "1.26.4";
            src = prev.fetchurl {
              url = "https://go.dev/dl/go${version}.src.tar.gz";
              hash = "sha256-T2aKMvv8ETLmqIH7lowvHa2mMUkqM5IRc1+7JVpCYC0=";
            };
          });
        };

        base = import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
          };
          overlays = [ goOverlay ];
        };
        unstable = import nixpkgs-unstable {
          inherit system;
          config = {
            allowUnfree = true;
          };
          overlays = [ goOverlay ];
        };

        pkgs = base // {
          inherit (unstable) buildGoModule;
        };
      in
      rec {
        packages = import ./nix {
          inherit self pkgs;
          inherit (pkgs) lib;
        };

        defaultPackage = packages.loki;

        apps = {
          lint = {
            type = "app";
            program =
              with pkgs;
              "${(writeShellScriptBin "lint.sh" ''
                ${nixpkgs-fmt}/bin/nixpkgs-fmt --check ${self}/flake.nix ${self}/nix/*.nix
                ${statix}/bin/statix check ${self}
              '')}/bin/lint.sh";
          };
        };

        devShell = pkgs.mkShell {
          nativeBuildInputs =
            with pkgs;
            [
              (pkgs.callPackage ./nix/packages/chart-releaser.nix {
                inherit pkgs;
                inherit (pkgs) buildGoModule fetchFromGitHub;
              })

              chart-testing
              gcc
              go_1_26
              golangci-lint
              gotools
              helm-docs
              nettools
              nixpkgs-fmt
              statix
              yamllint
            ]
            ++ (builtins.attrValues packages);
        };
      }
    );
}
