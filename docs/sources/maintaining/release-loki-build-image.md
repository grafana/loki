---
title: Releasing Loki Build Image
description: Releasing Loki Build Image
---
# Releasing Loki Build Image

The [`loki-build-image`](https://github.com/grafana/loki/tree/main/loki-build-image)
is the Docker image used to run tests and build Grafana Loki binaries in CI.

The build and publish process of the image is triggered upon a merge to `main`
if there were made any changes in the folder `./loki-build-image/`.

**To build and use the `loki-build-image`:**

## Step 1

1. create a branch with the desired changes to the Dockerfile
2. update the version tag of the `loki-build-image` pipeline defined in `.drone/drone.jsonnet` (search for `pipeline('loki-build-image')`) to a new version number (try follow semver)
3. run `DRONE_SERVER=https://drone.grafana.net/ DRONE_TOKEN=<token> make drone` and commit the changes to the same branch
   1. the `<token>` is your personal drone token, which can be found by navigating to https://drone.grafana.net/account.
4. create a PR
5. once approved and merged to `main`, the image with the new version is built and published
   - **hint:** keep an eye on https://drone.grafana.net/grafana/loki for the build after merging ([example](https://drone.grafana.net/grafana/loki/17760/1/2))

## Step 2

1. create a branch
2. update the `BUILD_IMAGE_VERSION` variable in the `Makefile`
3. Repeat step 1.3, which will use the new image
4. run `loki-build-image/version-updater.sh <new-version>` to update all the references

5. run `DRONE_SERVER=https://drone.grafana.net/ DRONE_TOKEN=<token> make drone` to update the Drone config to use the new build image
6. create a PR

