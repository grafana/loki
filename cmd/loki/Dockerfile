FROM golang:1.17.2 as build

COPY . /src/loki
WORKDIR /src/loki

# Copying this from loki-build-image/Dockerfile
# because these dependencies are required by the Makefile in certain
# circumstances. This should be removed and the base image above changed
# from golang to loki-build-image once the latter becomes available for arm.
RUN GO111MODULE=on go install \
    # Due to the lack of a proper release tag, we use the commit hash of
    # https://github.com/golang/tools/releases v0.1.7
    golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69 && \
    rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache

RUN make clean && make BUILD_IN_CONTAINER=false loki

FROM alpine:3.13

RUN apk add --no-cache ca-certificates libcap

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-docker-config.yaml /etc/loki/local-config.yaml

RUN addgroup -g 10001 -S loki && \
    adduser -u 10001 -S loki -G loki
RUN mkdir -p /loki/rules && \
    mkdir -p /loki/rules-temp && \
    chown -R loki:loki /etc/loki /loki

# See https://github.com/grafana/loki/issues/1928
RUN [ ! -e /etc/nsswitch.conf ] && echo 'hosts: files dns' > /etc/nsswitch.conf

USER loki
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]

