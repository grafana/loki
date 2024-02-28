FROM golang:1.21.3 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-querytee

FROM alpine:3.18.6
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/querytee/querytee /usr/bin/querytee
ENTRYPOINT [ "/usr/bin/querytee" ]
