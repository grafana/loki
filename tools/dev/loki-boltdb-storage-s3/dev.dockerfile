FROM golang:1.20.1
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.9.0

FROM alpine:3.16.4

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
