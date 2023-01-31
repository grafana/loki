{ pkgs, version, imageTag }:
pkgs.stdenv.mkDerivation {
  inherit version;

  pname = "loki";

  src = ./..;

  buildInputs = with pkgs; [
    bash
    gcc
    go
    git
    bash
    systemd
    yamllint
    nettools

    golangci-lint
    (import ./faillint.nix {
      inherit (pkgs) lib buildGoModule fetchFromGitHub;
    })
  ];

  configurePhase = with pkgs; ''
    patchShebangs tools

    substituteInPlace Makefile \
      --replace "SHELL = /usr/bin/env bash -o pipefail" "SHELL = ${bash}/bin/bash -o pipefail" \
      --replace "IMAGE_TAG := \$(shell ./tools/image-tag)" "IMAGE_TAG := ${imageTag}" \
      --replace "GIT_REVISION := \$(shell git rev-parse --short HEAD)" "GIT_REVISION := ${version}" \
      --replace "GIT_BRANCH := \$(shell git rev-parse --abbrev-ref HEAD)" "GIT_BRANCH := nix" \

    substituteInPlace clients/cmd/fluentd/Makefile \
      --replace "SHELL    = /usr/bin/env bash -o pipefail" "SHELL = ${bash}/bin/bash -o pipefail"
  '';

  buildPhase = ''
    export GOCACHE=$TMPDIR/go-cache
    make clean loki logcli loki-canary promtail
  '';

  doCheck = true;
  checkPhase = ''
    export GOCACHE=$TMPDIR/go-cache
    export GOLANGCI_LINT_CACHE=$TMPDIR/go-cache
    make lint test
  '';

  installPhase = ''
    mkdir -p $out/bin
    install -m755 cmd/loki/loki $out/bin/loki
    install -m755 cmd/logcli/logcli $out/bin/logcli
    install -m755 cmd/loki-canary/loki-canary $out/bin/loki-canary
    install -m755 clients/cmd/promtail/promtail $out/bin/promtail
  '';
}
