---
title: Releasing Loki Build Image
---
# Releasing `loki-build-image`

The [`loki-build-image`](https://github.com/grafana/loki/tree/master/loki-build-image)
is the Docker image used to run tests and build Grafana Loki binaries in CI.

The build and publish process of the image is triggered upon a merge to `main`
if there were made any changes in the folder `./loki-build-image/`.

**Building and using the `loki-build-image` is a two-step process.**

As a **first step** to build the new image, you need to create a pull
request with the desired changes to the Dockerfile. To increase the version of
the image, you also need to update the version tag of the `loki-build-image`
pipeline defined in `.drone/drone.jsonnet` (search for
`pipeline('loki-build-image')`) and run `DRONE_SERVER=https://drone.grafana.net/ DRONE_TOKEN=<token> make drone`
and commit the changes to the same pull request.
Once approved and merged to `main`, the image with the new version is built.

The new image can only be used after updating the `BUILD_IMAGE_VERSION` in the
`Makefile` a **second step**. After changing the version in the Makefile and
updating it in all other places where the image is used:

* Dockerfiles in `cmd` directory
* `.circleci/config.yml`

run `BUILD_IN_CONTAINER=false make drone` again and submit a PR with the
generated changes.
