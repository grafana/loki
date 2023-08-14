FROM golang:1.20.6 as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-canary

FROM alpine:3.16.7
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/loki-canary/loki-canary-boringcrypto /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
