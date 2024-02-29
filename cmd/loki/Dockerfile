FROM golang:1.21.3 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki

FROM alpine:3.18.6

RUN apk add --no-cache ca-certificates libcap

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-docker-config.yaml /etc/loki/local-config.yaml

RUN addgroup -g 10001 -S loki && \
    adduser -u 10001 -S loki -G loki
RUN mkdir -p /loki/rules && \
    mkdir -p /loki/rules-temp && \
    chown -R loki:loki /etc/loki /loki

USER 10001
EXPOSE 3100
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
