ARG BUILD_IMAGE=grafana/loki-build-image:latest
# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/promtail -f cmd/promtail/Dockerfile .
FROM golang:1.11.4-alpine as goenv
RUN go env GOARCH > /goarch && \
  go env GOARM > /goarm

FROM --platform=linux/amd64 $BUILD_IMAGE as build
COPY --from=goenv /goarch /goarm /
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && GOARCH=$(cat /goarch) GOARM=$(cat /goarm) make BUILD_IN_CONTAINER=false loki-canary

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /go/src/github.com/grafana/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
