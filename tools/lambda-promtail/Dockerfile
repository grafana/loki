FROM golang:1.22.2-alpine AS build-image
# RUN yum install -y golang
RUN apk update && apk add git
RUN go version
RUN go env -w GOPROXY=direct
ADD . .
RUN go build -o /main -tags lambda.norpc -ldflags="-s -w" lambda-promtail/*.go
RUN ls -al
# copy artifacts to a clean image
FROM public.ecr.aws/lambda/provided:al2
COPY --from=build-image /main /main
ENTRYPOINT [ "/main" ]       