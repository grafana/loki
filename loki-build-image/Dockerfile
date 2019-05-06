FROM golang:1.11.4-stretch
RUN apt-get update && apt-get install -y file jq unzip protobuf-compiler libprotobuf-dev && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
ENV DOCKER_VER="17.03.0-ce"
RUN curl -L -o /tmp/docker-$DOCKER_VER.tgz https://download.docker.com/linux/static/stable/x86_64/docker-$DOCKER_VER.tgz && \
    tar -xz -C /tmp -f /tmp/docker-$DOCKER_VER.tgz && \
    mv /tmp/docker/* /usr/bin && \
		rm /tmp/docker-$DOCKER_VER.tgz
ENV HELM_VER="v2.13.1"
RUN curl -L -o /tmp/helm-$HELM_VER.tgz http://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz && \
    tar -xz -C /tmp -f /tmp/helm-$HELM_VER.tgz && \
    mv /tmp/linux-amd64/helm /usr/bin/helm && \
    rm -rf /tmp/linux-amd64 /tmp/helm-$HELM_VER.tgz
RUN go get \
		github.com/golang/protobuf/protoc-gen-go \
		github.com/gogo/protobuf/protoc-gen-gogoslick \
		github.com/gogo/protobuf/gogoproto \
		github.com/go-delve/delve/cmd/dlv \
		golang.org/x/tools/cmd/goyacc && \
		rm -rf /go/pkg /go/src
ENV GOLANGCI_LINT_COMMIT="692dacb773b703162c091c2d8c59f9cd2d6801db"
RUN mkdir -p $(go env GOPATH)/src/github.com/golangci/ && git clone https://github.com/golangci/golangci-lint.git $(go env GOPATH)/src/github.com/golangci/golangci-lint && \
	cd $(go env GOPATH)/src/github.com/golangci/golangci-lint  && git checkout ${GOLANGCI_LINT_COMMIT} && cd cmd/golangci-lint/  &&\
	GO111MODULE=on go install && \
	golangci-lint help
COPY build.sh /
ENV GOCACHE=/go/cache
ENTRYPOINT ["/build.sh"]
