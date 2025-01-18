# This is the Dockerfile for the Loki build image that is used by the CI
# pipelines.
# If you make changes to this Dockerfile you also need to update the
# tag of the Docker image in `../.drone/drone.jsonnet` and run `make drone`.
# See ../docs/sources/community/maintaining/release-loki-build-image.md for instructions
# on how to publish a new build image.
ARG GO_VERSION=1.23
ARG GOLANG_BASE_IMAGE=golang:${GO_VERSION}-bullseye

# Install helm (https://helm.sh/) and helm-docs (https://github.com/norwoodj/helm-docs) for generating Helm Chart reference.
FROM ${GOLANG_BASE_IMAGE} AS helm
ARG TARGETARCH
ARG HELM_VER="v3.2.3"
RUN curl -L "https://get.helm.sh/helm-${HELM_VER}-linux-$TARGETARCH.tar.gz" | tar zx && \
    install -t /usr/local/bin "linux-$TARGETARCH/helm"
RUN BIN=$([ "$TARGETARCH" = "arm64" ] && echo "helm-docs_Linux_arm64" || echo "helm-docs_Linux_x86_64") &&  \
    curl -L "https://github.com/norwoodj/helm-docs/releases/download/v1.11.2/$BIN.tar.gz" | tar zx && \
    install -t /usr/local/bin helm-docs

FROM alpine:3.21.2 AS lychee
ARG TARGETARCH
ARG LYCHEE_VER="0.7.0"
RUN apk add --no-cache curl && \
    curl -L -o /tmp/lychee-$LYCHEE_VER.tgz https://github.com/lycheeverse/lychee/releases/download/${LYCHEE_VER}/lychee-${LYCHEE_VER}-x86_64-unknown-linux-gnu.tar.gz && \
    tar -xz -C /tmp -f /tmp/lychee-$LYCHEE_VER.tgz && \
    mv /tmp/lychee /usr/bin/lychee && \
    rm -rf "/tmp/linux-$TARGETARCH" /tmp/lychee-$LYCHEE_VER.tgz

FROM alpine:3.21.2 AS golangci
RUN apk add --no-cache curl && \
    cd / && \
    curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.60.3

FROM alpine:3.21.2 AS buf
ARG TARGETOS
RUN apk add --no-cache curl && \
    curl -sSL "https://github.com/bufbuild/buf/releases/download/v1.4.0/buf-$TARGETOS-$(uname -m)" -o "/usr/bin/buf" && \
    chmod +x "/usr/bin/buf"

FROM alpine:3.21.2 AS docker
RUN apk add --no-cache docker-cli docker-cli-buildx

FROM ${GOLANG_BASE_IMAGE} AS drone
ARG TARGETARCH
RUN curl -L "https://github.com/drone/drone-cli/releases/download/v1.7.0/drone_linux_$TARGETARCH".tar.gz | tar zx && \
    install -t /usr/local/bin drone

# Install faillint used to lint go imports in CI.
# This collisions with the version of go tools used in the base image, thus we install it in its own image and copy it over.
# Error:
# github.com/fatih/faillint@v1.5.0 requires golang.org/x/tools@v0.0.0-20200207224406-61798d64f025
#   (not golang.org/x/tools@v0.0.0-20190918214920-58d531046acd from golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69)
FROM ${GOLANG_BASE_IMAGE} AS faillint
RUN GO111MODULE=on go install github.com/fatih/faillint@v1.12.0
RUN GO111MODULE=on go install golang.org/x/tools/cmd/goimports@v0.7.0

FROM ${GOLANG_BASE_IMAGE} AS delve
RUN GO111MODULE=on go install github.com/go-delve/delve/cmd/dlv@latest

# Install ghr used to push binaries and template the release
# This collides with the version of go tools used in the base image, thus we install it in its own image and copy it over.
FROM ${GOLANG_BASE_IMAGE} AS ghr
RUN GO111MODULE=on go install github.com/tcnksm/ghr@9349474

# Install nfpm (https://nfpm.goreleaser.com) for creating .deb and .rpm packages.
FROM ${GOLANG_BASE_IMAGE} AS nfpm
RUN GO111MODULE=on go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.11.3

# Install gotestsum
FROM ${GOLANG_BASE_IMAGE} AS gotestsum
RUN GO111MODULE=on go install gotest.tools/gotestsum@v1.8.2

# Install tools used to compile jsonnet.
FROM ${GOLANG_BASE_IMAGE} AS jsonnet
RUN GO111MODULE=on go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@v0.5.1
RUN GO111MODULE=on go install github.com/monitoring-mixins/mixtool/cmd/mixtool@16dc166166d91e93475b86b9355a4faed2400c18
RUN GO111MODULE=on go install github.com/google/go-jsonnet/cmd/jsonnet@v0.20.0

FROM aquasec/trivy AS trivy

FROM ${GOLANG_BASE_IMAGE}
RUN apt-get update && \
    apt-get install -qy \
    musl gnupg ragel \
    file zip unzip jq gettext\
    protobuf-compiler libprotobuf-dev \
    libsystemd-dev jq && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Install dependencies to cross build Promtail to ARM and ARM64.
RUN dpkg --add-architecture armhf && \
    dpkg --add-architecture arm64 && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
    pkg-config \
    gcc-aarch64-linux-gnu libc6-dev-arm64-cross libsystemd-dev:arm64 \
    gcc-arm-linux-gnueabihf libc6-dev-armhf-cross libsystemd-dev:armhf && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=docker /usr/bin/docker /usr/bin/docker
COPY --from=docker /usr/libexec/docker/cli-plugins/docker-buildx /usr/libexec/docker/cli-plugins/docker-buildx
COPY --from=helm /usr/local/bin/helm /usr/bin/helm
COPY --from=helm /usr/local/bin/helm-docs /usr/bin/helm-docs
COPY --from=lychee /usr/bin/lychee /usr/bin/lychee
COPY --from=golangci /bin/golangci-lint /usr/local/bin
COPY --from=buf /usr/bin/buf /usr/bin/buf
COPY --from=drone /usr/local/bin/drone /usr/bin/drone
COPY --from=faillint /go/bin/faillint /usr/bin/faillint
COPY --from=faillint /go/bin/goimports /usr/bin/goimports
COPY --from=delve /go/bin/dlv /usr/bin/dlv
COPY --from=ghr /go/bin/ghr /usr/bin/ghr
COPY --from=nfpm /go/bin/nfpm /usr/bin/nfpm
COPY --from=gotestsum /go/bin/gotestsum /usr/bin/gotestsum
COPY --from=jsonnet /go/bin/jb /usr/bin/jb
COPY --from=jsonnet /go/bin/mixtool /usr/bin/mixtool
COPY --from=jsonnet /go/bin/jsonnet /usr/bin/jsonnet
COPY --from=trivy /usr/local/bin/trivy /usr/bin/trivy

# Install some necessary dependencies.
# Forcing GO111MODULE=on is required to specify dependencies at specific versions using the go mod notation.
# If we don't force this, Go is going to default to GOPATH mode as we do not have an active project or go.mod
# file for it to detect and switch to Go Modules automatically.
# It's possible this can be revisited in newer versions of Go if the behavior around GOPATH vs GO111MODULES changes
RUN GO111MODULE=on go install github.com/golang/protobuf/protoc-gen-go@v1.3.1
RUN GO111MODULE=on go install github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0
# Due to the lack of a proper release tag, we use the commit hash of
# https://github.com/golang/tools/releases v0.1.7
RUN GO111MODULE=on go install golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69
RUN GO111MODULE=on go install github.com/mitchellh/gox@9f71238 && rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache
ENV GOTEST="gotestsum --format testname --"

COPY build.sh /
RUN chmod +x /build.sh
ENTRYPOINT ["/build.sh"]
