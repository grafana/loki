FROM golang:1.21.3-alpine AS build-image

COPY tools/lambda-promtail /src/lambda-promtail
WORKDIR /src/lambda-promtail

RUN go version

RUN apk update && apk upgrade && \
    apk add --no-cache bash git

RUN go mod download
RUN go build -o ./main -tags lambda.norpc -ldflags="-s -w" lambda-promtail/*.go


FROM alpine:3.18.6

WORKDIR /app

COPY --from=build-image /src/lambda-promtail/main ./

ENTRYPOINT ["/app/main"]
