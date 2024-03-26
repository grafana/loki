{ pkgs, lib, buildGoModule, dockerTools, rev }:
rec {
  loki-helm-test = buildGoModule rec {
    pname = "loki-helm-test";
    version = "0.1.0";

    src = ./../../../../..;
    vendorHash = null;

    buildPhase = ''
      runHook preBuild
      go test --tags=helm_test -c -o $out/bin/helm-test ./production/helm/loki/src/helm-test
      runHook postBuild
      '';

    doCheck = false;
  };

  # by default, uses the nix hash as the tag, which can be retrieved with:
  # basename "$(readlink result)" | cut -d - -f 1
  loki-helm-test-docker = dockerTools.buildImage {
    name = "grafana/loki-helm-test";
    config = {
      Entrypoint = [ "${loki-helm-test}/bin/helm-test" ];
    };
  };
}
