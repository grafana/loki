---
title: Loki Build Image
description: What the Loki build image is and how to change it.
aliases: 
- ../../maintaining/release-loki-build-image/
---
# Loki Build Image

The [`loki-build-image`](https://github.com/grafana/loki/blob/main/loki-build-image)
provides the toolchain used to build, lint, test, and release Grafana Loki: a Go
toolchain plus tools such as `golangci-lint`, `goyacc`, `goimports`, `mixtool`,
`buf`, `helm`, and the packaging tools (`fpm`, `rpm`).

It is defined by
[`loki-build-image/Dockerfile`](https://github.com/grafana/loki/blob/main/loki-build-image/Dockerfile),
which builds on the `golang:<GO_VERSION>` base image and installs the tooling via
`.github/vendor/github.com/grafana/loki-release/workflows/install_workflow_dependencies.sh`.

## How it is used

The image is **built locally on demand** — it is not published from this repository:

- Running any `make` target with `BUILD_IN_CONTAINER=true` builds the Dockerfile and
  runs the target inside the resulting container.
- The
  [dev container](https://github.com/grafana/loki/blob/main/.devcontainer/devcontainer.json)
  builds the same Dockerfile for local development.

CI builds Loki directly on the `golang:<GO_VERSION>` image and does not pull a
pre-published build image.

## Making a change

- **Change the Go version:** update `GO_VERSION` in the `Makefile`. The build image
  and the generated GitHub workflows derive their Go version from it. See
  [Patch Go version](../release/patch-go-version/) for the full procedure.
- **Change the toolchain:** edit `install_workflow_dependencies.sh` (and the
  `Dockerfile` if needed). The image is rebuilt automatically the next time a
  containerized `make` target or the dev container is built — there is nothing to
  publish.
