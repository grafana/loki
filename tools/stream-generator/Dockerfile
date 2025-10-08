ARG GO_VERSION=1.24

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o stream-generator ./tools/stream-generator/main.go

# Prepare filesystem stage
FROM golang:${GO_VERSION} AS filesystem
RUN addgroup --gid 10001 streamgenerator && \
    adduser --uid 10001 --gid 10001 --disabled-password --gecos "" streamgenerator

# Final stage
FROM gcr.io/distroless/static

COPY --from=build /src/loki/stream-generator /usr/bin/stream-generator
COPY --from=filesystem /etc/passwd /etc/passwd
COPY --from=filesystem /etc/group /etc/group

USER 10001
EXPOSE 9090
ENTRYPOINT [ "/usr/bin/stream-generator" ]
