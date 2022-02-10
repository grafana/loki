---
title: Releasing Loki Build Image
---
# Releasing `loki-build-image`

The [`loki-build-image`](https://github.com/grafana/loki/tree/master/loki-build-image) is the Docker image used to run tests and build Grafana Loki binaries in CI.

The image is released with any change to `./loki-build-image` on `main`.

## How To Use a New Release

1. Update the version tag of the `loki-build-image` pipeline defined in `.drone/drone.jsonnet` (search for `pipeline('loki-build-image')`).
1. Merge change into `main` and wait for the release.
1. Update `BUILD_IMAGE_VERSION` in the `Makefile`.
1. Update the image version in all the other places it exists
    1. Dockerfiles in `cmd` directory
    1. .circleci/config.yml
1. Run `make drone BUILD_IN_CONTAINER=false` to rebuild the drone yml file with the new image version (the image version in the Makefile is used)
2. Merge change into `main`.
