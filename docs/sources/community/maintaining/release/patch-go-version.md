---
title: Patch Go version
description: Patch Go version
---
# Patch Go version

Update vulnerable Go version to non-vulnerable Go version to build Grafana Loki binaries.

## Before you begin.

1. Determine the [VERSION_PREFIX]({{< relref "./concepts/version" >}}).

1. Need to sign-in to docker hub to be able to push Loki build image.

## Steps

1. Find Go version to which you need to update. Example `1.20.5` to `1.20.6`

1. Update Go version in the Grafana Loki build image(`loki-build-image/Dockerfile`) on the `main` branch.

1. [Backport]({{< relref "./backport-commits" >}}) it to `release-VERSION_PREFIX` branch

1. Determine new Grafana Loki build image version.

   1. Check current `BUILD_IMAGE_VERSION` from `Makefile`.

   1. Increment 1 to the last digit. Example `v0.29.4` becomes `v0.29.5`

   1. This will be your new `NEW_BUILD_IMAGE_VERSION`.

1. Build and push new Grafana Loki build image.

   ```shell
   BUILD_IMAGE_VERSION=$NEW_BUILD_IMAGE_VERSION make build-image-push
   ```

1. Update build image versions on `main` branch.

   1. Update `BUILD_IMAGE_VERSION` to `$NEW_BUILD_IMAGE_VERSION` in `Makefile`

   1. Update drone file.
	   ```shell
	   make .drone/drone.yml
	   ```

1. [Backport]({{< relref "./backport-commits" >}}) it to `release-VERSION_PREFIX` branch
