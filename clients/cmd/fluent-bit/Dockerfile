FROM golang:1.23-bullseye AS builder

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

FROM fluent/fluent-bit:3.2.2@sha256:b7a2b9ee1958607da9a16b0efeb24a70a43c5b7e91dbabca4dc02e916aadb914

COPY --from=builder /src/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
