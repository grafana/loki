{ pkgs, self }:
let
  buildVars = import ./build-vars.nix;

  # self.rev is only set on a clean git tree
  gitRevision = if (self ? rev) then self.rev else buildVars.gitRevision;

  # version is the "short" git sha
  version = with pkgs.lib;
    (strings.concatStrings (lists.take 8 (strings.stringToCharacters gitRevision)));

  imageTag = "${buildVars.gitBranch}-${
      if (self ? rev) then version else buildVars.gitRevision
    }";
in
{
  loki = import ./loki.nix {
    inherit pkgs version imageTag;
    inherit (buildVars) gitBranch;
  };
}
