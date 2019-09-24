FROM golang:1.12 as build
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make BUILD_IN_CONTAINER=false fluent-bit-plugin

FROM fluent/fluent-bit:1.0
COPY --from=build /go/src/github.com/grafana/loki/fluent-bit/fluent-bit-go-loki/out_loki.so /fluent-bit/bin
EXPOSE 2020
CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
