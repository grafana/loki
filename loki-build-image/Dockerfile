FROM golang:1.11.2-stretch
RUN apt-get update && apt-get install -y file jq unzip protobuf-compiler libprotobuf-dev && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
ENV DOCKER_VER="17.03.0-ce"
RUN curl -L -o /tmp/docker-$DOCKER_VER.tgz https://download.docker.com/linux/static/stable/x86_64/docker-$DOCKER_VER.tgz && \
    tar -xz -C /tmp -f /tmp/docker-$DOCKER_VER.tgz && \
    mv /tmp/docker/* /usr/bin && \
		rm /tmp/docker-$DOCKER_VER.tgz
RUN go get \
		github.com/golang/protobuf/protoc-gen-go \
		github.com/gogo/protobuf/protoc-gen-gogoslick \
		github.com/gogo/protobuf/gogoproto \
		golang.org/x/tools/cmd/goyacc && \
		rm -rf /go/pkg /go/src
ENV GOMETALINTER_VER="2.0.11"
RUN curl -L -o /tmp/gometalinter-$GOMETALINTER_VER.tgz https://github.com/alecthomas/gometalinter/releases/download/v$GOMETALINTER_VER/gometalinter-$GOMETALINTER_VER-linux-amd64.tar.gz && \
		tar -xz -C /tmp -f /tmp/gometalinter-$GOMETALINTER_VER.tgz && \
		mv /tmp/gometalinter-$GOMETALINTER_VER-linux-amd64/* /usr/bin && \
		rm /tmp/gometalinter-$GOMETALINTER_VER.tgz
COPY build.sh /
ENV GOCACHE=/go/cache
ENTRYPOINT ["/build.sh"]
