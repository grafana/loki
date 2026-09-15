ARG GO_VERSION=1.27@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o stream-generator ./tools/stream-generator/main.go

# Final stage
FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/stream-generator /usr/bin/stream-generator

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S streamgenerator && \
    adduser -u 10001 -S streamgenerator -G streamgenerator && \
    chown -R streamgenerator:streamgenerator /usr/bin/stream-generator && \
    ln -s /busybox/sh /bin/sh

USER 10001
EXPOSE 9090
ENTRYPOINT [ "/usr/bin/stream-generator" ]
