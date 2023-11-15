{ pkgs, version, imageTag }:
pkgs.stdenv.mkDerivation {
  inherit version;

  pname = "loki";

  src = ./../..;

  buildInputs = with pkgs; [
    bash
    gcc
    git
    go
    golangci-lint
    nettools
    systemd
    yamllint

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
    make clean loki
  '';

  doCheck = false;
  checkPhase = ''
    export GOCACHE=$TMPDIR/go-cache
    export GOLANGCI_LINT_CACHE=$TMPDIR/go-cache
    make lint test
  '';

  installPhase = ''
    mkdir -p $out/bin
    install -m755 cmd/loki/loki $out/bin/loki
  '';
}
