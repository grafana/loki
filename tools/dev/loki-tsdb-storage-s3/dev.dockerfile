FROM golang:1.24
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.24.2

FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
