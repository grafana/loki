ARG GO_VERSION=1.26.5@sha256:705e964a93a2fd2e75c7d59bb7d781b57e30f12293ffde5175c69229e18fb678

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o stream-generator ./tools/stream-generator/main.go

# Final stage
FROM gcr.io/distroless/static:debug@sha256:fdd11b2ee83d2cd35774fcb1c94c76e57fd4470da0c26d5748a91fc2faf7b9b1

COPY --from=build /src/loki/stream-generator /usr/bin/stream-generator

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S streamgenerator && \
    adduser -u 10001 -S streamgenerator -G streamgenerator && \
    chown -R streamgenerator:streamgenerator /usr/bin/stream-generator && \
    ln -s /busybox/sh /bin/sh

USER 10001
EXPOSE 9090
ENTRYPOINT [ "/usr/bin/stream-generator" ]
