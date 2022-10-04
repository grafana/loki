{ pkgs, lib, buildGoModule, dockerTools, rev }:
rec {
  helm-test = buildGoModule rec {
    pname = "loki-helm-test";
    version = "0.1.0";

    src = ./../../../../..;
    vendorSha256 = null;

    buildPhase = ''
      runHook preBuild
      go test -c -o $out/bin/helm-test ./production/helm/loki/src/helm-test
      runHook postBuild
      '';

    doCheck = false;
  };

  # by default, uses the nix hash as the tag, which can be retrieved with:
  # basename "$(readlink result)" | cut -d - -f 1
  helm-test-docker = dockerTools.buildImage {
    name = "grafana/loki-helm-test";
    config = {
      Entrypoint = [ "${helm-test}/bin/helm-test" ];
    };
  };
}
