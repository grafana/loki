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
RUN go get \
    github.com/golang/protobuf/protoc-gen-go \
    github.com/gogo/protobuf/protoc-gen-gogoslick \
    github.com/gogo/protobuf/gogoproto \
    github.com/go-delve/delve/cmd/dlv \
    golang.org/x/tools/cmd/goyacc \
    github.com/mitchellh/gox \
    github.com/tcnksm/ghr && \
    rm -rf /go/pkg /go/src
ENV GOCACHE=/go/cache

COPY build.sh /
RUN chmod +x /build.sh
ENTRYPOINT ["/build.sh"]
