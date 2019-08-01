ARG BUILD_IMAGE=grafana/loki-build-image:0.2.1
# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/loki -f cmd/loki/Dockerfile .
FROM golang:1.11.4-alpine as goenv
RUN go env GOARCH > /goarch && \
    go env GOARM > /goarm

FROM --platform=linux/amd64 $BUILD_IMAGE as build
COPY --from=goenv /goarch /goarm /
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN GOARCH=$(cat /goarch) GOARM=$(cat /goarm) make clean && make loki

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /go/src/github.com/grafana/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE 80
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
