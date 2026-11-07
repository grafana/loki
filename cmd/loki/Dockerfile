ARG GO_VERSION=1.26@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c

# Go build stage
FROM golang:${GO_VERSION} AS build
ARG IMAGE_TAG
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} loki

# Prepare filesystem stage
FROM golang:${GO_VERSION} AS filesystem
COPY cmd/loki/loki-docker-config.yaml /local-config.yaml
RUN mkdir -p /etc/loki /loki/rules /loki/rules-temp && \
    cp /local-config.yaml /etc/loki/local-config.yaml && \
    addgroup --gid 10001 loki && \
    adduser --uid 10001 --gid 10001 --disabled-password --gecos "" loki && \
    chown -R loki:loki /etc/loki /loki

# Final stage
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY --from=filesystem --chown=10001:10001 /etc/loki /etc/loki
COPY --from=filesystem --chown=10001:10001 /loki /loki
COPY --from=filesystem /etc/passwd /etc/passwd
COPY --from=filesystem /etc/group /etc/group

USER 10001
WORKDIR /
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
