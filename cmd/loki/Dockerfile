FROM golang:1.13 as build
# TOUCH_PROTOS signifies if we should touch the compiled proto files and thus not regenerate them.
# This is helpful when file system timestamps can't be trusted with make
ARG TOUCH_PROTOS
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && (if [ "${TOUCH_PROTOS}" ]; then make touch-protos; fi) && make BUILD_IN_CONTAINER=false loki

FROM alpine:3.9

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-local-config.yaml /etc/loki/local-config.yaml

RUN apk add --no-cache ca-certificates libcap && \
    setcap cap_net_bind_service=+ep /usr/bin/loki && \
    apk del --no-cache libcap

RUN addgroup -g 10001 -S loki && \
    adduser -u 10001 -S loki -G loki && \
    mkdir -p /data && \
    chown -R loki:loki /etc/loki /data

USER loki
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
