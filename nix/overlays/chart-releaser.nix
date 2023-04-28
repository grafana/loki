prev: final: {
  chart-releaser = prev.callPackage ../packages/chart-releaser.nix {
    inherit (prev) pkgs lib buildGoModule fetchFromGitHub;
  };
}
