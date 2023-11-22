{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  # Nixpkgs / NixOS version to use.

  outputs = { self, nixpkgs, flake-utils }:
    let
      nix = import ./nix { inherit self; };
    in
    {
      overlays = {
        default = nix.overlay;
      };
    } //
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            nix.overlay
          ];
          config = { allowUnfree = true; };
        };
      in
      {
        # The default package for 'nix build'. This makes sense if the
        # flake provides only one package or there is a clear "main"
        # package.
        defaultPackage = pkgs.loki;

        packages = with pkgs; {
          inherit
            logcli
            loki
            loki-canary
            loki-helm-test
            loki-helm-test-docker
            promtail;
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
            program = with pkgs; "${
                (writeShellScriptBin "test.sh" ''
                  ${loki.overrideAttrs(old: { doCheck = true; })}/bin/loki --version
                '')
              }/bin/test.sh";
          };

          loki = {
            type = "app";
            program = with pkgs; "${loki}/bin/loki";
          };
          promtail = {
            type = "app";
            program = with pkgs; "${promtail}/bin/promtail";
          };
          logcli = {
            type = "app";
            program = with pkgs; "${logcli}/bin/logcli";
          };
          loki-canary = {
            type = "app";
            program = with pkgs; "${loki-canary}/bin/loki-canary";
          };
          loki-helm-test = {
            type = "app";
            program = with pkgs; "${loki-helm-test}/bin/helm-test";
          };
        };

        devShell = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            (import ./packages/chart-releaser.nix {
              inherit (prev) pkgs lib buildGoModule fetchFromGitHub;
            })

            chart-testing
            faillint
            gcc
            go
            golangci-lint
            gotools
            helm-docs
            nettools
            nixpkgs-fmt
            statix
            systemd
            yamllint
          ];
        };
      });
}
