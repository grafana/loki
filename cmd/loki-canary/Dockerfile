# Directories in this file are referenced from the root of the project not this folder
# This file is intented to be called from the root like so:
# docker build -t grafana/promtail -f cmd/promtail/Dockerfile .

FROM grafana/loki-build-image:0.2.1 as build
ARG GOARCH="amd64"
COPY . /go/src/github.com/grafana/loki
WORKDIR /go/src/github.com/grafana/loki
RUN make clean && make loki-canary

FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       --from=build /go/src/github.com/grafana/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
ENTRYPOINT [ "/usr/bin/loki-canary" ]
