FROM golang:1.16.2 as build

ARG TOUCH_PROTOS
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-querytee

FROM alpine:3.13
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/querytee/loki-querytee /usr/bin/querytee
ENTRYPOINT [ "/usr/bin/querytee" ]
