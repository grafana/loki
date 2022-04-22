FROM golang:1.17.9 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-querytee

FROM alpine:3.15.4
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/querytee/querytee /usr/bin/querytee
ENTRYPOINT [ "/usr/bin/querytee" ]
