{ self }:
{
  overlay = final: prev:
    let
      # self.rev is only set on a clean git tree
      gitRevision = if (self ? rev) then self.rev else "dirty";
      shortGitRevsion = with prev.lib;
        if (self ? rev) then
          (strings.concatStrings
            (lists.take 8 (strings.stringToCharacters gitRevision)))
        else
          "dirty";

      # the image tag script is hard coded to take only 7 characters
      imageTagVersion = with prev.lib;
        if (self ? rev) then
          (strings.concatStrings
            (lists.take 8 (strings.stringToCharacters gitRevision)))
        else
          "dirty";

      imageTag =
        if (self ? rev) then
          "${imageTagVersion}"
        else
          "${imageTagVersion}-WIP";

      loki-helm-test = prev.callPackage ../production/helm/loki/src/helm-test {
        inherit (prev) pkgs lib buildGoModule dockerTools;
        rev = gitRevision;
      };
    in
    {
      inherit (loki-helm-test) loki-helm-test loki-helm-test-docker;
    } // rec {
      loki = prev.callPackage ./packages/loki.nix {
        inherit imageTag;
        version = shortGitRevsion;
        pkgs = prev;
      };

      logcli = loki.overrideAttrs (oldAttrs: {
        pname = "logcli";

        buildPhase = ''
          export GOCACHE=$TMPDIR/go-cache
          make clean logcli
        '';

        installPhase = ''
          mkdir -p $out/bin
          install -m755 cmd/logcli/logcli $out/bin/logcli
        '';
      });

      loki-canary = loki.overrideAttrs (oldAttrs: {
        pname = "loki-canary";

        buildPhase = ''
          export GOCACHE=$TMPDIR/go-cache
          make clean loki-canary
        '';

        installPhase = ''
          mkdir -p $out/bin
          install -m755 cmd/loki-canary/loki-canary $out/bin/loki-canary
        '';
      });

      promtail = loki.overrideAttrs (oldAttrs: {
        pname = "promtail";

        buildInputs = if prev.stdenv.hostPlatform.isLinux then oldAttrs.buildInputs ++ [ prev.systemd ] else oldAttrs.buildInputs;

        buildPhase = ''
          export GOCACHE=$TMPDIR/go-cache
          make clean promtail
        '';

        installPhase = ''
          mkdir -p $out/bin
          install -m755 clients/cmd/promtail/promtail $out/bin/promtail
        '';
      });
    };
}
