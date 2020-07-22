---
title: Releasing Loki Build Image
---
# Releasing `loki-build-image`

The [`loki-build-image`](https://github.com/grafana/loki/tree/master/loki-build-image) is the Docker image used to run tests and build Loki binaries in CI.

## How To Perform a Release

1. Update `BUILD_IMAGE_VERSION` in the `Makefile`
1. Update the image version in all the other places it exists
    1. Dockerfiles in `cmd` directory
    1. .circleci/config.yml
1. Run `make drone` to rebuild the drone yml file with the new image version (the image version in the Makefile is used)
1. Commit your changes (else you will get a WIP tag)
2. Run `make build-image-push`
