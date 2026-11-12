FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS builder

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

FROM fluent/fluent-bit:5.2.0@sha256:a48190bf67679495a332fd61025421a46e7e4d168380185a8e16da2e9acc3126

COPY --from=builder /src/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
