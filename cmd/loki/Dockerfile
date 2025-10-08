ARG GO_VERSION=1.24

# Go build stage
FROM golang:${GO_VERSION} AS build
ARG IMAGE_TAG
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} loki

# Prepare filesystem stage
FROM golang:${GO_VERSION} AS filesystem
COPY cmd/loki/loki-docker-config.yaml /tmp/local-config.yaml
RUN mkdir -p /tmp/etc/loki /tmp/loki/rules /tmp/loki/rules-temp && \
    cp /tmp/local-config.yaml /tmp/etc/loki/local-config.yaml && \
    addgroup --gid 10001 loki && \
    adduser --uid 10001 --gid 10001 --disabled-password --gecos "" loki && \
    chown -R loki:loki /tmp/etc/loki /tmp/loki

# Final stage
FROM gcr.io/distroless/static

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY --from=filesystem /tmp/etc/loki /etc/loki
COPY --from=filesystem /tmp/loki /loki
COPY --from=filesystem /etc/passwd /etc/passwd
COPY --from=filesystem /etc/group /etc/group

USER 10001
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
