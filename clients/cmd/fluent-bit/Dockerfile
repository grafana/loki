FROM golang:1.20.6@sha256:832f2f74baa3e2b00ace688cb2fa934dffeade39f5b4c0cc8b1cff8d3fb084a0 AS builder

FROM fluent/fluent-bit:1.8
COPY --from=build /src/loki/clients/cmd/fluent-bit/out_grafana_loki.so /fluent-bit/bin
COPY clients/cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf
EXPOSE 2020
CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_grafana_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
