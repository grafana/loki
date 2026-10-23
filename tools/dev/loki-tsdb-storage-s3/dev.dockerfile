FROM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.27.2

FROM alpine:3.24.3@sha256:b3413572228e34bd1c9245f5565e6cd127005655e8a7899aef33d8e83255c9e6

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
