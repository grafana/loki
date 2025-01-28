ARG GO_VERSION=1.23

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o stream-meta-gen ./tools/streametagen/main.go

# Final stage
FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/stream-meta-gen /usr/bin/stream-meta-gen

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S streammetagen && \
    adduser -u 10001 -S streammetagen -G streammetagen && \
    chown -R streammetagen:streammetagen /usr/bin/stream-meta-gen && \
    ln -s /busybox/sh /bin/sh

USER 10001
EXPOSE 9090
ENTRYPOINT [ "/usr/bin/stream-meta-gen" ] 