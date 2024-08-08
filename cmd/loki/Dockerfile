ARG GO_VERSION=1.22
FROM golang:${GO_VERSION} as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki

FROM gcr.io/distroless/base-nossl:debug

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-docker-config.yaml /etc/loki/local-config.yaml

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S loki && \
    adduser -u 10001 -S loki -G loki
RUN mkdir -p /loki/rules && \
    mkdir -p /loki/rules-temp && \
    chown -R loki:loki /etc/loki /loki && \
    ln -s /busybox/sh /bin/sh
USER 10001
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
