---
title: Patch Go version
description: Describes the procedure how to patch the Go version in the Loki build image.
---
# Patch Go version

Update vulnerable Go version to non-vulnerable Go version to build Grafana Loki binaries.

## Before you begin.

1. Determine the [VERSION_PREFIX](../concepts/version/).

## Steps

1. Find Go version to which you need to update. Example `1.20.5` to `1.20.6`.

1. On the `main` branch, update `GO_VERSION` in the `Makefile`. This is the source of
   truth for the [Loki build image](../../release-loki-build-image/) and the generated
   GitHub workflows.

1. Run `tools/go-version-bump.sh <version>` to update the remaining `FROM golang:<ver>`
   Dockerfiles and the dev container.

1. Run `make release-workflows` to regenerate the workflow files that embed the Go
   version.

1. Update any remaining hardcoded Go versions, for example the `GO_VERSION` entries in
   the hand-maintained `.github/workflows/` files and the `go` directive in `go.mod`.

1. [Backport](../backport-commits/) the changes to the `release-VERSION_PREFIX` branch.
