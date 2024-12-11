{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
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

          test = {
            type = "app";
            program =
              let
                loki = packages.loki.overrideAttrs (old: {
                  buildInputs = with pkgs; lib.optionals stdenv.hostPlatform.isLinux [ systemd.dev ];
                  doCheck = true;
                  checkFlags = [
                    "-covermode=atomic"
                    "-coverprofile=coverage.txt"
                    "-p=4"
                  ];
                  subPackages = [
                    "./..." # for tests
                    "cmd/loki"
                    "cmd/logcli"
                    "cmd/loki-canary"
                    "clients/cmd/promtail"
                  ];
                });
              in
              "${
                (pkgs.writeShellScriptBin "test.sh" ''
                  ${loki}/bin/loki --version
                  ${loki}/bin/logcli --version
                  ${loki}/bin/loki-canary --version
                  ${loki}/bin/promtail --version
                '')
              }/bin/test.sh";
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
