FROM golang:1.22.5
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.20.2

FROM alpine:3.20.2

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./