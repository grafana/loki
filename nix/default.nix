{ pkgs, self }:
let
  buildVars = import ./build-vars.nix;

  # self.rev is only set on a clean git tree
  gitRevision = if (self ? rev) then self.rev else buildVars.gitRevision;

  # version is the "short" git sha
  version = with pkgs.lib;
    (strings.concatStrings
      (lists.take 8 (strings.stringToCharacters gitRevision)));

  # the image tag script is hard coded to take only 7 characters
  imageTagVersion = with pkgs.lib;
    (strings.concatStrings
      (lists.take 7 (strings.stringToCharacters gitRevision)));

  imageTag =
    if (self ? rev) then
      "${buildVars.gitBranch}-${imageTagVersion}"
    else
      "${buildVars.gitBranch}-${imageTagVersion}-WIP";
in
{
  loki = import ./loki.nix {
    inherit pkgs version imageTag;
    inherit (buildVars) gitBranch;
  };
}
