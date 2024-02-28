FROM golang:1.21.3 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && CGO_ENABLED=0 go build ./cmd/logql-analyzer/

FROM alpine:3.18.6

RUN apk add --no-cache ca-certificates

COPY --from=build /src/loki/logql-analyzer /usr/bin/logql-analyzer

ENTRYPOINT [ "/usr/bin/logql-analyzer" ]
