ARG GO_VERSION=1.24

# Go build stage
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build -o ratestore-standalone ./tools/ratestore-standalone/main.go

# Final stage
FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/ratestore-standalone /usr/bin/ratestore-standalone

SHELL [ "/busybox/sh", "-c" ]

RUN addgroup -g 10001 -S ratestore-standalone && \
    adduser -u 10001 -S ratestore-standalone -G ratestore-standalone && \
    chown -R ratestore-standalone:ratestore-standalone /usr/bin/ratestore-standalone && \
    ln -s /busybox/sh /bin/sh

USER 10001
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/ratestore-standalone" ] 