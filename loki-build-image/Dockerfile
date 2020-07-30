FROM alpine as helm
ARG HELM_VER="v2.13.1"
RUN apk add --no-cache curl && \
    curl -L -o /tmp/helm-$HELM_VER.tgz http://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz && \
    tar -xz -C /tmp -f /tmp/helm-$HELM_VER.tgz && \
    mv /tmp/linux-amd64/helm /usr/bin/helm && \
    rm -rf /tmp/linux-amd64 /tmp/helm-$HELM_VER.tgz

FROM alpine as golangci
RUN apk add --no-cache curl && \
    cd / && \
    curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s v1.21.0

FROM alpine:edge as docker
RUN apk add --no-cache docker-cli

# TODO this should be fixed to download and extract the specific release binary from github as we do for golangci and helm above
# however we need a commit which hasn't been released yet: https://github.com/drone/drone-cli/commit/1fad337d74ca0ecf420993d9d2d7229a1c99f054
# Read the comment below regarding GO111MODULE=on and why it is necessary
FROM golang:1.14.2 as drone
RUN GO111MODULE=on go get github.com/drone/drone-cli/drone@1fad337d74ca0ecf420993d9d2d7229a1c99f054

# Install faillint used to lint go imports in CI.
# This collisions with the version of go tools used in the base image, thus we install it in its own image and copy it over.
# Error:
#	github.com/fatih/faillint@v1.5.0 requires golang.org/x/tools@v0.0.0-20200207224406-61798d64f025
#   (not golang.org/x/tools@v0.0.0-20190918214920-58d531046acd from golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69)
FROM golang:1.14.2 as faillint
RUN GO111MODULE=on go get github.com/fatih/faillint@v1.5.0

FROM golang:1.14.2-stretch
RUN apt-get update && \
    apt-get install -qy \
    musl gnupg \
    file zip unzip jq gettext\
    protobuf-compiler libprotobuf-dev \
    libsystemd-dev && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=docker /usr/bin/docker /usr/bin/docker
COPY --from=helm /usr/bin/helm /usr/bin/helm
COPY --from=golangci /bin/golangci-lint /usr/local/bin
COPY --from=drone /go/bin/drone /usr/bin/drone
COPY --from=faillint /go/bin/faillint /usr/bin/faillint

# Install some necessary dependencies.
# Forcing GO111MODULE=on is required to specify dependencies at specific versions using the go mod notation.
# If we don't force this, Go is going to default to GOPATH mode as we do not have an active project or go.mod
# file for it to detect and switch to Go Modules automatically.
# It's possible this can be revisited in newer versions of Go if the behavior around GOPATH vs GO111MODULES changes
RUN GO111MODULE=on go get \
    github.com/golang/protobuf/protoc-gen-go@v1.3.0 \
    github.com/gogo/protobuf/protoc-gen-gogoslick@v1.2.1 \
    github.com/gogo/protobuf/gogoproto@v1.2.1 \
    github.com/go-delve/delve/cmd/dlv@v1.3.2 \
    # Due to the lack of a proper release tag, we use the commit hash of
    # https://github.com/golang/tools/releases v0.1.7
    golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69 \
    github.com/mitchellh/gox \
    github.com/tcnksm/ghr && \
    rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache

COPY build.sh /
RUN chmod +x /build.sh
ENTRYPOINT ["/build.sh"]
