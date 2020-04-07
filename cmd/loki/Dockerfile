FROM golang:1.13 as build
# TOUCH_PROTOS signifies if we should touch the compiled proto files and thus not regenerate them.
# This is helpful when file system timestamps can't be trusted with make
ARG TOUCH_PROTOS
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && (if [ "${TOUCH_PROTOS}" ]; then make touch-protos; fi) && make BUILD_IN_CONTAINER=false loki

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates libcap \
    && rm -rf /var/cache/apk/*

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-local-config.yaml /etc/loki/local-config.yaml

RUN setcap cap_net_bind_service=+ep /usr/bin/loki

RUN apk del --no-cache libcap && rm -rf /var/cache/apk/*

RUN addgroup -g 1000 -S loki && \
    adduser -u 1000 -S loki -G loki
RUN mkdir -p /loki && \
    chown -R loki:loki /etc/loki /loki

USER loki
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
