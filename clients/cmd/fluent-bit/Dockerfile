FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS builder

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

FROM fluent/fluent-bit:5.1.1@sha256:a941bdd5ca552b2c6597fc7b3bccf2e61d30873939bab5700963de0e94ac6169

COPY --from=builder /src/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
