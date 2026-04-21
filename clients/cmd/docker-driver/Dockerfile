ARG GO_VERSION=1.26
ARG BUILD_IMAGE=golang:${GO_VERSION}
ARG GOARCH=amd64
# Directories in this file are referenced from the root of the project not this folder
# This file is intended to be called from the root like so:
# docker build -t grafana/loki-docker-driver -f clients/cmd/docker-driver/Dockerfile .

FROM $BUILD_IMAGE AS build
COPY . /src/loki
WORKDIR /src/loki

ARG GOARCH
RUN make clean && make BUILD_IN_CONTAINER=false GOARCH=${GOARCH} clients/cmd/docker-driver/docker-driver

FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS temp

ARG GOARCH

RUN apk add --update --no-cache --arch=${GOARCH} ca-certificates tzdata

FROM --platform=linux/${GOARCH} alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

COPY --from=temp /etc/ca-certificates.conf /etc/ca-certificates.conf
COPY --from=temp /usr/share/ca-certificates /usr/share/ca-certificates
COPY --from=temp /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=build /src/loki/clients/cmd/docker-driver/docker-driver /bin/docker-driver

WORKDIR /bin/
ENTRYPOINT [ "/bin/docker-driver" ]
