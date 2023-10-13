FROM golang:1.20.10 as build

COPY . /src/loki
WORKDIR /src/loki
RUN go env GOARCH > /goarch
RUN make clean && make GOARCH=$(cat /goarch) BUILD_IN_CONTAINER=true GOEXPERIMENT=boringcrypto loki-canary-boringcrypto

FROM alpine:3.18.3
RUN apk add --update --no-cache ca-certificates
RUN apk add --no-cache libc6-compat
COPY --from=build /src/loki/cmd/loki-canary-boringcrypto/loki-canary-boringcrypto /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
