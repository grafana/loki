---
title: Releasing Loki Build Image
description: Releasing Loki Build Image
aliases: 
- ../../maintaining/release-loki-build-image/
---
# Releasing Loki Build Image

The [`loki-build-image`](https://github.com/grafana/loki/blob/main/loki-build-image)
is the Docker image used to run tests and build Grafana Loki binaries in CI.

The build and publish process of the image is triggered upon a merge to `main`
if any changes were made in the folder `./loki-build-image/`.

**To build and use the `loki-build-image`:**

## Step 1

1. Create a branch with the desired changes to the Dockerfile.
2. Update the version tag of the `loki-build-image` pipeline defined in `.drone/drone.jsonnet` (search for `pipeline('loki-build-image')`) to a new version number (try to follow semver).
3. Run `DRONE_SERVER=https://drone.grafana.net/ DRONE_TOKEN=<token> make drone` and commit the changes to the same branch.
   1. The `<token>` is your personal drone token, which can be found by navigating to https://drone.grafana.net/account.
4. Create a PR.
5. Once approved and merged to `main`, the image with the new version is built and published.
   {{% admonition type="note" %}}
   Keep an eye on https://drone.grafana.net/grafana/loki for the build after merging ([example](https://drone.grafana.net/grafana/loki/17760/1/2)).
   {{% /admonition %}}

## Step 2

1. Create a branch.
2. Update the `BUILD_IMAGE_VERSION` variable in the `Makefile`.
3. Run `loki-build-image/version-updater.sh <new-version>` to update all the references.
4. Run `DRONE_SERVER=https://drone.grafana.net/ DRONE_TOKEN=<token> make drone` to update the Drone config to use the new build image.
5. Create a new PR.

