ARG BUILD_IMAGE=grafana/loki-build-image:0.33.6
# Directories in this file are referenced from the root of the project not this folder
# This file is intended to be called from the root like so:
# docker build -t grafana/loki -f cmd/loki/Dockerfile .

# TODO: add cross-platform support
FROM $BUILD_IMAGE AS build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false clients/cmd/docker-driver/docker-driver

FROM alpine:3.20.2
RUN apk add --update --no-cache ca-certificates tzdata
COPY --from=build /src/loki/clients/cmd/docker-driver/docker-driver /bin/docker-driver
WORKDIR /bin/
ENTRYPOINT [ "/bin/docker-driver" ]
