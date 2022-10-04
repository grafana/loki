FROM golang:1.18.5 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false helm-test

FROM alpine:3.16.2
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/production/helm/loki/src/helm-test/helm-test /usr/bin/helm-test
ENTRYPOINT [ "/usr/bin/helm-test" ]
