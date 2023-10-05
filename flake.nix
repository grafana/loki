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
        golangci-lint = import ./nix/overlays/golangci-lint.nix;
        helm-docs = import ./nix/overlays/helm-docs.nix;
        default = nix.overlay;
      };
    } //
    flake-utils.lib.eachDefaultSystem (system:
      let

        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (import ./nix/overlays/golangci-lint.nix)
            (import ./nix/overlays/helm-docs.nix)
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
            loki
            loki-helm-test
            loki-helm-test-docker;
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

          loki = {
            type = "app";
            program = with pkgs; "${loki.overrideAttrs(old: rec { doCheck = false; })}/bin/loki";
          };
          promtail = {
            type = "app";
            program = with pkgs; "${loki.overrideAttrs(old: rec { doCheck = false; })}/bin/promtail";
          };
          logcli = {
            type = "app";
            program = with pkgs; "${loki.overrideAttrs(old: rec { doCheck = false; })}/bin/logcli";
          };
          loki-canary = {
            type = "app";
            program = with pkgs; "${loki.overrideAttrs(old: rec { doCheck = false; })}/bin/loki-canary";
          };
          loki-helm-test = {
            type = "app";
            program = with pkgs; "${loki-helm-test}/bin/helm-test";
          };
        };

        devShell = pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            gcc
            go
            systemd
            yamllint
            nixpkgs-fmt
            statix
            nettools

            golangci-lint
            gotools
            helm-docs
            faillint
            chart-testing
            chart-releaser
          ];
        };
      });
}
