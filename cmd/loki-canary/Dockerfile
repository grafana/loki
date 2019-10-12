FROM golang:1.13 as build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-canary

FROM alpine:3.9
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
