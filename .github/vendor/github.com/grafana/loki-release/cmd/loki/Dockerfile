FROM golang:1.21.3 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make loki

FROM alpine:3.18.5

COPY --from=build /src/loki/cmd/loki/loki /usr/bin/loki

RUN addgroup -g 10001 -S loki && \
    adduser -u 10001 -S loki -G loki

USER 10001
EXPOSE 3100
CMD ["/usr/bin/loki"]
