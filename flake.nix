{
  description = "Grafana Loki";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  # Nixpkgs / NixOS version to use.

  outputs = { self, nixpkgs, flake-utils }:
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

                ldflags = [
                  "-s"
                  "-w"
                  "-X main.version=${version}"
                  "-X main.commit=v${version}"
                  "-X main.date=19700101-00:00:00"
                ];
              });
          };
      };

      helm-docs-overlay = final: prev: {
        helm-docs = prev.callPackage
          "${prev.path}/pkgs/applications/networking/cluster/helm-docs"
          {
            buildGoModule = args:
              prev.buildGoModule (args // rec {
                version = "1.11.0";

                src = prev.fetchFromGitHub {
                  owner = "norwoodj";
                  repo = "helm-docs";
                  rev = "v${version}";
                  sha256 = "sha256-476ZhjRwHlNJFkHzY8qQ7WbAUUpFNSoxXLGX9esDA/E=";
                };

                vendorSha256 = "sha256-xXwunk9rmzZEtqmSo8biuXnAjPp7fqWdQ+Kt9+Di9N8=";

                ldflags = [
                  "-w"
                  "-s"
                  "-X main.version=v${version}"
                ];
              });
          };
      };

      nix = import ./nix { inherit self; };
    in
    {
      overlays = {
        golangci-lint = golangci-lint-overlay;
        helm-docs = helm-docs-overlay;
        default = nix.overlay;
      };
    } //
    flake-utils.lib.eachDefaultSystem (system:
      let

        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            golangci-lint-overlay
            helm-docs-overlay
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
