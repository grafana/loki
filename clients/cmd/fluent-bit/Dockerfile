FROM golang:1.24-bullseye AS builder

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

FROM fluent/fluent-bit:4.2.0@sha256:aff743e1a5e61d036d10a4472d67cdd7a6b408c92218eb86e3f301e2bac7817a

COPY --from=builder /src/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
