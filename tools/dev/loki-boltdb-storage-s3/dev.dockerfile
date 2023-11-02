FROM golang:1.20.4
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.21.1

FROM alpine:3.18.3

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
