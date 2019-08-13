FROM golang:1.12 as build
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-canary

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /go/src/github.com/grafana/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
