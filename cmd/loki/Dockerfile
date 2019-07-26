# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/loki -f cmd/loki/Dockerfile .

FROM grafana/loki-build-image:0.2.1 as build
ARG GOARCH="amd64"
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make loki

FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       --from=build /go/src/github.com/grafana/loki/cmd/loki/loki /usr/bin/loki
COPY       cmd/loki/loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE     80
ENTRYPOINT [ "/usr/bin/loki" ]
CMD        ["-config.file=/etc/loki/local-config.yaml"]
