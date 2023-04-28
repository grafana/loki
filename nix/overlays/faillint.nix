final: prev: {
  faillint = prev.callPackage ../packages/faillint.nix { inherit (prev) lib buildGoModule fetchFromGitHub; };
}
