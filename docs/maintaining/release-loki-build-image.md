# Releasing `loki-build-image`

The [`loki-build-image`](../../loki-build-image/) is the Docker image used to run tests and build Loki binaries in CI.

## How To Perform a Release

1. Update `BUILD_IMAGE_VERSION` in the `Makefile`
2. Run `make build-image-push`
