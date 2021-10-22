FROM golang:1-alpine3.12 AS build-image

COPY tools/lambda-promtail /src/lambda-promtail
WORKDIR /src/lambda-promtail

RUN go version

RUN apk update && apk upgrade && \
    apk add --no-cache bash git

RUN go mod download
RUN go build -tags lambda.norpc -ldflags="-s -w" lambda-promtail/main.go


FROM alpine:3.12

WORKDIR /app

COPY --from=build-image /src/lambda-promtail/main ./

ENTRYPOINT ["/app/main"]