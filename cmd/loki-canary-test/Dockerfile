FROM golang:1.18.5 as build

COPY . /src/loki
WORKDIR /src/loki/cmd/loki-canary-test
RUN  go test ./...
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-canary-test

FROM alpine:3.16.2
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/loki-canary-test/loki-canary-test /usr/bin/loki-canary-test
ENTRYPOINT [ "/usr/bin/loki-canary-test" ]
