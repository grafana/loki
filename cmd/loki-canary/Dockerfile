FROM golang:1.17.2 as build

COPY . /src/loki
WORKDIR /src/loki

# Copying this from loki-build-image/Dockerfile
# because these dependencies are required by the Makefile in certain
# circumstances. This should be removed and the base image above changed
# from golang to loki-build-image once the latter becomes available for arm.
RUN GO111MODULE=on go install \
    # Due to the lack of a proper release tag, we use the commit hash of
    # https://github.com/golang/tools/releases v0.1.7
    golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69 && \
    rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache

RUN make clean && make BUILD_IN_CONTAINER=false loki-canary

FROM alpine:3.13
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
