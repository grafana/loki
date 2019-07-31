ARG BUILD_IMAGE=grafana/loki-build-image:0.2.1
# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/loki -f cmd/loki/Dockerfile .

# TODO: add cross-platform support
FROM $BUILD_IMAGE as build
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make cmd/docker-driver/docker-driver

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /go/src/github.com/grafana/loki/cmd/docker-driver/docker-driver /bin/docker-driver
WORKDIR /bin/
ENTRYPOINT [ "/bin/docker-driver" ]
