---
title: Patch Go version
description: Describes the procedure how to patch the Go version in the Loki build image.
---
# Patch Go version

Update vulnerable Go version to non-vulnerable Go version to build Grafana Loki binaries.

## Before you begin.

1. Determine the [VERSION_PREFIX](../concepts/version/).

1. Need to sign-in to Docker hub to be able to push Loki build image.

## Steps

1. Find Go version to which you need to update. Example `1.20.5` to `1.20.6`

1. Update Go version in the Grafana Loki build image (`loki-build-image/Dockerfile`) on the `main` branch.

1. [Release a new Loki Build Image](../../release-loki-build-image/)

1. [Backport](../backport-commits/) the Dockerfile change to `release-VERSION_PREFIX` branch.

1. [Backport](../backport-commits/) the Loki Build Image version change from `main` to `release-VERSION_PREFIX` branch.
