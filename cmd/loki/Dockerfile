FROM golang:1.12 as build
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /go/src/github.com/grafana/loki/cmd/loki/loki /usr/bin/loki
COPY cmd/loki/loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE 80
ENTRYPOINT [ "/usr/bin/loki" ]
CMD ["-config.file=/etc/loki/local-config.yaml"]
