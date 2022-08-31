{ pkgs, self }:
let
  buildVars = import ./build-vars.nix;

  # self.rev is only set on a clean git tree
  version = if (self ? rev) then self.rev else buildVars.gitRevision;
  shortVersion = with pkgs.lib;
    (strings.concatStrings (lists.take 8 (strings.stringToCharacters version)));
in
{
  loki = import ./loki.nix { inherit pkgs version shortVersion buildVars; };
}
