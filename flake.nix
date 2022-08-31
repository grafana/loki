{
  description = "Grafana Enterprise Logs";

  # Nixpkgs / NixOS version to use.
  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  inputs.flake-utils.url = "github:numtide/flake-utils";

  # find historical versions here: https://lazamar.co.uk/nix-versions or
  # check git history of NixOS/nixpkgs

  # # golang-ci @ 1.47.1
  inputs.golangci-lint-nixpkgs.url =
    "nixpkgs/87dc446e3d0e1d56bdd435268a9cb8596314ad81";

  # helm-docs @ 1.8.1
  inputs.helm-docs-nixpkgs.url =
    "nixpkgs/bf972dc380f36a3bf83db052380e55f0eaa7dcb6";

  outputs =
    { self, nixpkgs, flake-utils, helm-docs-nixpkgs, golangci-lint-nixpkgs }:
    let
      buildVars = import ./nix/build-vars.nix;
      # self.rev is only set on a clean git tree
      version = if (self ? rev) then self.rev else buildVars.gitRevision;
    in
    flake-utils.lib.eachDefaultSystem (system:
    let
      pkgs = import nixpkgs {
        inherit system;
        overlays = [
          (final: prev: {
            inherit (import golangci-lint-nixpkgs { inherit system; })
              golangci-lint;
          })
          (final: prev: {
            inherit (import helm-docs-nixpkgs { inherit system; }) helm-docs;
          })
        ];
        config = { allowUnfree = true; };
      };

      shortVersion = with pkgs.lib;
        (strings.concatStrings
          (lists.take 8 (strings.stringToCharacters version)));

      loki = pkgs.callPackage ./nix/loki.nix {
        inherit (pkgs) pkgs;
        inherit version shortVersion buildVars;
      };
    in
    {
      # The default package for 'nix build'. This makes sense if the
      # flake provides only one package or there is a clear "main"
      # package.
      defaultPackage = loki;

      packages = { inherit loki; };

      devShell = pkgs.mkShell {
        nativeBuildInputs = with pkgs; [
          gcc
          go
          systemd
          yamllint

          golangci-lint
          helm-docs

          loki
        ];

        shellHook = ''
          pushd $(git rev-parse --show-toplevel) > /dev/null || exit 1
          ./nix/generate-build-vars.sh
          popd > /dev/null || exit 1
        '';
      };
    });
}
