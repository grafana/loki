ARG GO_VERSION=1.26@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o stream-generator ./tools/stream-generator/main.go

# Final stage
FROM gcr.io/distroless/static:debug@sha256:63b4c0065ff7bd68753510400b2d5cedd5d5571d15eac4be0b6b0dd236a2d148

COPY --from=build /src/loki/stream-generator /usr/bin/stream-generator

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S streamgenerator && \
    adduser -u 10001 -S streamgenerator -G streamgenerator && \
    chown -R streamgenerator:streamgenerator /usr/bin/stream-generator && \
    ln -s /busybox/sh /bin/sh

USER 10001
EXPOSE 9090
ENTRYPOINT [ "/usr/bin/stream-generator" ]
