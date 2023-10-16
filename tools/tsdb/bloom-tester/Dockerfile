FROM golang:1.21.3 as build
ENV BUILD_IN_CONTAINER=false

COPY . /src/bloom-tester
WORKDIR /src/bloom-tester

RUN make bloom-tester

FROM alpine:3.18.3
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/bloom-tester/tools/tsdb/bloom-tester/bloom-tester /usr/bin/bloom-tester
ENTRYPOINT [ "/usr/bin/bloom-tester", "--config.file=/etc/loki/config.yaml" ]
#CMD tail -f /dev/null
