ARG BUILD_IMAGE=grafana/loki-build-image:0.29.3
# Directories in this file are referenced from the root of the project not this folder
# This file is intended to be called from the root like so:
# docker build -t grafana/promtail -f cmd/promtail/Dockerfile .
FROM golang:1.20.6-alpine as goenv
RUN go env GOARCH > /goarch && \
  go env GOARM > /goarm

FROM --platform=linux/amd64 $BUILD_IMAGE as build
COPY --from=goenv /goarch /goarm /
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && GOARCH=$(cat /goarch) GOARM=$(cat /goarm) BUILD_IN_CONTAINER=true GOEXPERIMENT=boringcrypto make loki-canary-boringcrypto

FROM alpine:3.16.5
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/loki-canary/loki-canary-boringcrypto /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
