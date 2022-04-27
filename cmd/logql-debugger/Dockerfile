FROM golang:1.17.9 as build

COPY . /src/loki
WORKDIR /src/loki
RUN CGO_ENABLED=0 go build ./cmd/logql-debugger/

FROM alpine:3.15.4

RUN apk add --no-cache ca-certificates

COPY --from=build /src/loki/logql-debugger /usr/bin/logql-debugger

ENTRYPOINT [ "/usr/bin/logql-debugger", "--server.http-listen-port=3001" ]