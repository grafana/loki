# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/promtail -f cmd/promtail/Dockerfile .

FROM grafana/loki-build-image:0.2.1 as build
ARG GOARCH="amd64"
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make promtail

FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates tzdata
COPY       --from=build /go/src/github.com/grafana/loki/cmd/promtail/promtail /usr/bin/promtail
COPY       cmd/promtail/promtail-local-config.yaml /etc/promtail/local-config.yaml
COPY       cmd/promtail/promtail-docker-config.yaml /etc/promtail/docker-config.yaml
ENTRYPOINT ["/usr/bin/promtail"]
