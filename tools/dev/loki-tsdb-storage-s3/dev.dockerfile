FROM golang:1.24
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.24.2

FROM alpine:3.23.7@sha256:ff7f0c727dd796aa36787bfcd9a64bf35a6ce72a73451b3359fde849b6703d63

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
