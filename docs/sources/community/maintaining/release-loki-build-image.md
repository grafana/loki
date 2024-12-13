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

1. Create a branch with the desired changes to the `./loki-build-image/Dockerfile`.
1. Update the `BUILD_IMAGE_VERSION` variable in the `Makefile`.
1. Commit your changes.
1. Run `make build-image-push` to build and publish the new version of the build image.
1. Run `make release-workflows` to update the Github workflows.
1. Commit your changes.
1. Push your changes to the remote branch.
1. Open a PR against the `main` branch.
