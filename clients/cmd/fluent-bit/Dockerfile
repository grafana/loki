FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS builder

COPY . /src

WORKDIR /src

ARG LDFLAGS
ENV CGO_ENABLED=1

RUN go build \
    -trimpath -ldflags "${LDFLAGS}" \
    -tags netgo \
    -buildmode=c-shared \
    -o clients/cmd/fluent-bit/out_grafana_loki.so \
    /src/clients/cmd/fluent-bit

FROM fluent/fluent-bit:5.1.5@sha256:2740f387f6e68fab66d28e4fa6a853387b9cab6ffb4cb78958f69bba4eb0e7b4

COPY --from=builder /src/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
