# This is the Dockerfile for the Loki build image that is used by the CI
# pipelines.
# If you make changes to this Dockerfile you also need to update the
# tag of the Docker image in `../.drone/drone.jsonnet` and run `make drone`.
# See ../docs/sources/maintaining/release-loki-build-image.md

FROM alpine as helm
ARG HELM_VER="v3.2.3"

RUN apk add --no-cache curl && \
    curl -L -o /tmp/helm-$HELM_VER.tgz https://get.helm.sh/helm-${HELM_VER}-linux-amd64.tar.gz && \
    tar -xz -C /tmp -f /tmp/helm-$HELM_VER.tgz && \
    mv /tmp/linux-amd64/helm /usr/bin/helm && \
    rm -rf /tmp/linux-amd64 /tmp/helm-$HELM_VER.tgz

FROM alpine as lychee
ARG LYCHEE_VER="0.7.0"
RUN apk add --no-cache curl && \
    curl -L -o /tmp/lychee-$LYCHEE_VER.tgz https://github.com/lycheeverse/lychee/releases/download/${LYCHEE_VER}/lychee-${LYCHEE_VER}-x86_64-unknown-linux-gnu.tar.gz && \
    tar -xz -C /tmp -f /tmp/lychee-$LYCHEE_VER.tgz && \
    mv /tmp/lychee /usr/bin/lychee && \
    rm -rf /tmp/linux-amd64 /tmp/lychee-$LYCHEE_VER.tgz

FROM alpine as golangci
RUN apk add --no-cache curl && \
    cd / && \
    curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.45.2

FROM alpine:3.15.4 as buf

RUN apk add --no-cache curl && \
    curl -sSL "https://github.com/bufbuild/buf/releases/download/v1.4.0/buf-$(uname -s)-$(uname -m)" -o "/usr/bin/buf" && \
    chmod +x "/usr/bin/buf"

FROM alpine:3.15.4 as docker
RUN apk add --no-cache docker-cli

# TODO this should be fixed to download and extract the specific release binary from github as we do for golangci and helm above
# however we need a commit which hasn't been released yet: https://github.com/drone/drone-cli/commit/1fad337d74ca0ecf420993d9d2d7229a1c99f054
# Read the comment below regarding GO111MODULE=on and why it is necessary
FROM golang:1.17.9 as drone
RUN curl -L https://github.com/drone/drone-cli/releases/download/v1.4.0/drone_linux_amd64.tar.gz | tar zx && \
    install -t /usr/local/bin drone

# Install faillint used to lint go imports in CI.
# This collisions with the version of go tools used in the base image, thus we install it in its own image and copy it over.
# Error:
#	github.com/fatih/faillint@v1.5.0 requires golang.org/x/tools@v0.0.0-20200207224406-61798d64f025
#   (not golang.org/x/tools@v0.0.0-20190918214920-58d531046acd from golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69)
FROM golang:1.17.9 as faillint
RUN GO111MODULE=on go get github.com/fatih/faillint@v1.5.0

FROM golang:1.17.9 as delve
RUN GO111MODULE=on go get github.com/go-delve/delve/cmd/dlv@v1.7.3

# Install ghr used to push binaries and template the release
# This collides with the version of go tools used in the base image, thus we install it in its own image and copy it over.
FROM golang:1.17.9 as ghr
RUN GO111MODULE=on go get github.com/tcnksm/ghr

# Install nfpm (https://nfpm.goreleaser.com) for creating .deb and .rpm packages.
FROM golang:1.17.9 as nfpm
RUN GO111MODULE=on go get github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.11.3

FROM golang:1.17.9-buster
RUN apt-get update && \
    apt-get install -qy \
    musl gnupg ragel \
    file zip unzip jq gettext\
    protobuf-compiler libprotobuf-dev \
    libsystemd-dev jq && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=docker /usr/bin/docker /usr/bin/docker
COPY --from=helm /usr/bin/helm /usr/bin/helm
COPY --from=lychee /usr/bin/lychee /usr/bin/lychee
COPY --from=golangci /bin/golangci-lint /usr/local/bin
COPY --from=buf /usr/bin/buf /usr/bin/buf
COPY --from=drone /usr/local/bin/drone /usr/bin/drone
COPY --from=faillint /go/bin/faillint /usr/bin/faillint
COPY --from=delve /go/bin/dlv /usr/bin/dlv
COPY --from=ghr /go/bin/ghr /usr/bin/ghr
COPY --from=nfpm /go/bin/nfpm /usr/bin/nfpm

# Install some necessary dependencies.
# Forcing GO111MODULE=on is required to specify dependencies at specific versions using the go mod notation.
# If we don't force this, Go is going to default to GOPATH mode as we do not have an active project or go.mod
# file for it to detect and switch to Go Modules automatically.
# It's possible this can be revisited in newer versions of Go if the behavior around GOPATH vs GO111MODULES changes
RUN GO111MODULE=on go get \
    github.com/golang/protobuf/protoc-gen-go@v1.3.1 \
    github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0 \
    github.com/gogo/protobuf/gogoproto@v1.3.0 \
    # Due to the lack of a proper release tag, we use the commit hash of
    # https://github.com/golang/tools/releases v0.1.7
    golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69 \
    github.com/mitchellh/gox && \
    rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache

COPY build.sh /
RUN chmod +x /build.sh
ENTRYPOINT ["/build.sh"]
