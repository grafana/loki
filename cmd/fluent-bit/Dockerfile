FROM golang:1.12 as build
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make BUILD_IN_CONTAINER=false fluent-bit-plugin

FROM fluent/fluent-bit:1.2
COPY --from=build /go/src/github.com/grafana/loki/cmd/fluent-bit/out_loki.so /fluent-bit/bin
COPY cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf
EXPOSE 2020
CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
