{ pkgs, version, imageTag }:
let
  lambda-promtail-gomod = pkgs.buildGoModule {
    inherit version;
    pname = "lambda-promtail";

    src = ./../../tools/lambda-promtail;
    vendorHash = "sha256-CKob173T0VHD5c8F26aU7p1l+QzqddNM4qQedMbLJa0=";

    doCheck = false;

    installPhase = ''
      runHook preInstall
      cp -r --reflink=auto vendor $out
      runHook postInstall
    '';
  };
in
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
    yamllint

    (import ./faillint.nix {
      inherit (pkgs) lib buildGoModule fetchFromGitHub;
    })
  ];

  configurePhase = with pkgs; ''
    patchShebangs tools

    substituteInPlace Makefile \
      --replace "SHELL = /usr/bin/env bash -o pipefail" "SHELL = ${bash}/bin/bash -o pipefail" \
      --replace "IMAGE_TAG ?= \$(shell ./tools/image-tag)" "IMAGE_TAG ?= ${imageTag}" \
      --replace "GIT_REVISION := \$(shell git rev-parse --short HEAD)" "GIT_REVISION := ${version}" \
      --replace "GIT_BRANCH := \$(shell git rev-parse --abbrev-ref HEAD)" "GIT_BRANCH := nix"

    substituteInPlace clients/cmd/fluentd/Makefile \
      --replace "SHELL    = /usr/bin/env bash -o pipefail" "SHELL = ${bash}/bin/bash -o pipefail"
  '';

  buildPhase = ''
    export GOCACHE=$TMPDIR/go-cache
    export GOMODCACHE=$TMPDIR/gomodcache
    export GOPROXY=off

    cp -r ${lambda-promtail-gomod} tools/lambda-promtail/vendor
    make clean loki
  '';

  doCheck = false;
  checkPhase = ''
    export GOCACHE=$TMPDIR/go-cache
    export GOMODCACHE=$TMPDIR/gomodcache
    export GOLANGCI_LINT_CACHE=$TMPDIR/go-cache
    export GOPROXY=off
    export BUILD_IN_CONTAINER=false

    make lint test
  '';

  installPhase = ''
    mkdir -p $out/bin
    install -m755 cmd/loki/loki $out/bin/loki
  '';
}
