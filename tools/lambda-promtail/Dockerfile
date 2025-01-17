FROM golang:1.22-alpine AS build-image

COPY tools/lambda-promtail /src/lambda-promtail
WORKDIR /src/lambda-promtail

RUN go version

RUN apk update && apk upgrade && \
    apk add --no-cache bash git
RUN go version

RUN ls -al
RUN go mod download
RUN go build -o /main -tags lambda.norpc -ldflags="-s -w" lambda-promtail/*.go
# copy artifacts to a clean image
FROM public.ecr.aws/lambda/provided:al2
RUN yum -y update openssl-libs ca-certificates krb5-libs
COPY --from=build-image /main /main
ENTRYPOINT [ "/main" ]
