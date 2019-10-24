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
    curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s v1.20.0

FROM alpine:edge as docker
RUN apk add --no-cache docker-cli

FROM golang:1.13.1-stretch
RUN apt-get update && \
    apt-get install -qy \
      musl \
      file unzip jq gettext\
      protobuf-compiler libprotobuf-dev \
      libsystemd-dev && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=docker /usr/bin/docker /usr/bin/docker
COPY --from=helm /usr/bin/helm /usr/bin/helm
COPY --from=golangci /bin/golangci-lint /usr/local/bin

# Enable go 1.11 modules to be able to install pinned version of dependencies
# even if we're installing them in the GOPATH
RUN GO111MODULE=on go get \
    github.com/golang/protobuf/protoc-gen-go@v1.3.0 \
    github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0 \
    github.com/gogo/protobuf/gogoproto@v1.3.0 \
    github.com/go-delve/delve/cmd/dlv \
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
