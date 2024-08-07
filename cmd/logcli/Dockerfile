FROM golang:1.22.5 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false logcli

FROM alpine:3.20.2

RUN apk add --no-cache ca-certificates

COPY --from=build /src/loki/cmd/logcli/logcli /usr/bin/logcli

ENTRYPOINT [ "/usr/bin/logcli" ]
