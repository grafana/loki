# These may be overwritten by --build-arg to build promtail or debug images
ARG TYPE=production

# ca-certificates
FROM alpine:3.9 as ssl
RUN apk add --update --no-cache ca-certificates

# use grafana/loki-build-image to compile binaries
FROM grafana/loki-build-image as golang
ARG GOARCH="amd64"
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN touch loki-build-image/.uptodate &&\
    mkdir /build

# production image
FROM golang as build-production
ARG APP
RUN make BUILD_IN_CONTAINER=false cmd/promtail/promtail &&\
  mv cmd/promtail/promtail /build/promtail

FROM scratch as production
COPY --from=ssl /etc/ssl /etc/ssl
COPY --from=build-production /build/promtail /usr/bin/promtail

# debug image (only amd64 supported, because of go-delve/delve#118)
FROM golang as build-debug
ARG APP
RUN go get github.com/go-delve/delve/cmd/dlv &&\
  make BUILD_IN_CONTAINER=false cmd/promtail/promtail-debug &&\
  mv cmd/promtail/promtail-debug /build/app-debug &&\
  mv cmd/promtail/dlv /build/dlv

FROM alpine:3.9 as debug
COPY --from=ssl /etc/ssl /etc/ssl
COPY --from=build-debug /build/app-debug /usr/bin/app-debug
COPY --from=build-debug /build/dlv /usr/bin/dlv
RUN apk add --no-cache libc6-compat
EXPOSE 40000
ENTRYPOINT ["/usr/bin/dlv", "--listen=:40000", "--headless=true", "--api-version=2", "exec", "/usr/bin/app-debug", "--"]

# final image with configuration
FROM ${TYPE}
COPY cmd/promtail/promtail-local-config.yaml cmd/promtail/promtail-docker-config.yaml /etc/promtail/
ENTRYPOINT ["/usr/bin/promtail"]
