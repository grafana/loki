{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-25.11";
    nixpkgs-unstable.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-unstable,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        # Override go_1_26 with upstream 1.26.3 — go.mod requires >=1.26.3 but
        # nixpkgs hasn't packaged it yet. Drop this overlay once nixpkgs catches up.
        goOverlay = _final: prev: {
          go_1_26 = prev.go_1_26.overrideAttrs (_: rec {
            version = "1.26.3";
            src = prev.fetchurl {
              url = "https://go.dev/dl/go${version}.src.tar.gz";
              hash = "sha256-HGRoddCqh5kTMYTtV895/yS97+jIggRwYCqdPW2Rkrg=";
            };
          });
          buildGo126Module = prev.buildGo126Module.override { go = _final.go_1_26; };
          buildGoModule = prev.buildGoModule.override { go = _final.go_1_26; };
        };

        base = import nixpkgs {
          inherit system;
          overlays = [ goOverlay ];
          config = {
            allowUnfree = true;
          };
        };
        unstable = import nixpkgs-unstable {
          inherit system;
          overlays = [ goOverlay ];
          config = {
            allowUnfree = true;
          };
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
