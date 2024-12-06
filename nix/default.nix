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

        subPackages = [ "cmd/logcli" ];

        meta = with prev.lib; {
          description = "LogCLI is a command line tool for interacting with Loki.";
          mainProgram = "logcli";
          license = with licenses; [ agpl3Only ];
          homepage = "https://grafana.com/oss/loki/";
          changelog = "https://github.com/grafana/loki/commit/${version}";
          maintainers = with maintainers; [ trevorwhitney ];
        };
      });

      loki-canary = loki.overrideAttrs (oldAttrs: {
        pname = "loki-canary";

        subPackages = [ "cmd/loki-canary" ];

        meta = with prev.lib; {
          description = "Loki Canary is a canary for the Loki project.";
          mainProgram = "loki-canary";
          license = with licenses; [ agpl3Only ];
          homepage = "https://grafana.com/oss/loki/";
          changelog = "https://github.com/grafana/loki/commit/${version}";
          maintainers = with maintainers; [ trevorwhitney ];
        };
      });

      promtail = loki.overrideAttrs (oldAttrs: {
        pname = "promtail";

        buildInputs = with prev; lib.optionals stdenv.hostPlatform.isLinux [ systemd.dev ];

        tags = [ "promtail_journal_enabled" ];

        subPackages = [ "clients/cmd/promtail" ];

        meta = with prev.lib; {
          description = "Like Prometheus, but for logs";
          mainProgram = "loki";
          license = with licenses; [ asl20 ];
          homepage = "https://grafana.com/oss/loki/";
          changelog = "https://github.com/grafana/loki/commit/${version}";
          maintainers = with maintainers; [ trevorwhitney ];
        };
      });
    };
}
