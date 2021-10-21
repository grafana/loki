FROM golang:1-alpine3.12 AS build-image

WORKDIR /app

RUN go version

RUN apk update && apk upgrade && \
    apk add --no-cache bash git

COPY go.mod go.sum ./
RUN go mod download

COPY lambda-promtail/main.go main.go
RUN go build -tags lambda.norpc -ldflags="-s -w" main.go


FROM alpine:3.12

WORKDIR /app

COPY --from=build-image /app/main ./

ENTRYPOINT ["/app/main"]