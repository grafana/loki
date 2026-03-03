FROM golang:1.26
ENV CGO_ENABLED=0
RUN go install github.com/go-delve/delve/cmd/dlv@v1.26.0

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

RUN     mkdir /loki
WORKDIR /loki
ADD     ./loki ./
ADD     ./.src ./src
COPY --from=0 /go/bin/dlv ./
