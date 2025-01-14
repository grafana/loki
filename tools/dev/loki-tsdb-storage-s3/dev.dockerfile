FROM golang:1.23
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.23.1

FROM alpine:3.21.2

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
