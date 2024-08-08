ARG BUILD_IMAGE=grafana/loki-build-image:0.33.4
ARG GOARCH=amd64
# Directories in this file are referenced from the root of the project not this folder
# This file is intended to be called from the root like so:
# docker build -t grafana/loki-docker-driver -f clients/cmd/docker-driver/Dockerfile .

FROM $BUILD_IMAGE as build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false clients/cmd/docker-driver/docker-driver

FROM alpine:3.18.5 as temp

RUN apk add --update --no-cache --arch=${GOARCH} ca-certificates tzdata

FROM --platform=linux/${GOARCH} alpine:3.18.5

COPY --from=temp /etc/ca-certificates.conf /etc/ca-certificates.conf
COPY --from=temp /usr/share/ca-certificates /usr/share/ca-certificates
COPY --from=temp /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=build /src/loki/clients/cmd/docker-driver/docker-driver /bin/docker-driver

WORKDIR /bin/
ENTRYPOINT [ "/bin/docker-driver" ]
