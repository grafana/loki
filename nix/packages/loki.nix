{ pkgs, version, imageTag, lib }:
pkgs.buildGo124Module {
  inherit version;

  pname = "loki";

  src = ./../..;
  vendorHash = null;

  ldflags =
    let
      prefix = "github.com/grafana/loki/v3/pkg/util/build";
    in
    [
      "-s"
      "-w"
      "-X ${prefix}.Branch=nix"
      "-X ${prefix}.Version=${imageTag}"
      "-X ${prefix}.Revision=${version}"
      "-X ${prefix}.BuildUser=nix@nixpkgs"
      "-X ${prefix}.BuildDate=unknown"
    ];

  subPackages = [ "cmd/loki" ];

  nativeBuildInputs = with pkgs; [ makeWrapper ];

  doCheck = false;

  meta = with lib; {
    description = "Like Prometheus, but for logs";
    mainProgram = "loki";
    license = with licenses; [ agpl3Only ];
    homepage = "https://grafana.com/oss/loki/";
    changelog = "https://github.com/grafana/loki/commit/${version}";
    maintainers = with maintainers; [ trevorwhitney ];
  };
}
