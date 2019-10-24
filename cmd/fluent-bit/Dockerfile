FROM golang:1.13 as build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false fluent-bit-plugin

FROM fluent/fluent-bit:1.2
COPY --from=build /src/loki/cmd/fluent-bit/out_loki.so /fluent-bit/bin
COPY cmd/fluent-bit/fluent-bit.conf /fluent-bit/etc/fluent-bit.conf
EXPOSE 2020
CMD ["/fluent-bit/bin/fluent-bit", "-e","/fluent-bit/bin/out_loki.so", "-c", "/fluent-bit/etc/fluent-bit.conf"]
