{ pkgs, lib, buildGoModule, dockerTools, rev }:
rec {
  loki-canary-test = buildGoModule rec {
    pname = "loki-canary-test";
    version = "0.1.0";

    src = ./../..;
    vendorSha256 = null;
    subPackages = [ "cmd/loki-canary-test" ];

    ldflags = [
      "-w"
      "-s"
      "-X github.com/grafana/loki/pkg/util/build.Version=${version}"
      "-X github.com/grafana/loki/pkg/util/build.Revision=${rev}"
    ];

    doCheck = true;
  };

  # by default, uses the nix hash as the tag, which can be retrieved with:
  # basename "$(readlink result)" | cut -d - -f 1
  loki-canary-test-docker = dockerTools.buildImage {
    name = "grafana/loki-canary-test";
    config = {
      Entrypoint = [ "${loki-canary-test}/bin/loki-canary-test" ];
    };
  };
}
