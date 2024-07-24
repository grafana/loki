ARG GO_VERSION=1.22
FROM golang:${GO_VERSION}-bookworm as build

COPY . /src/loki
WORKDIR /src/loki
RUN apt-get update && apt-get install -qy libsystemd-dev
RUN make clean && make BUILD_IN_CONTAINER=false PROMTAIL_JOURNAL_ENABLED=true promtail

# Promtail requires debian as the base image to support systemd journal reading
FROM debian:12.5-slim
# tzdata required for the timestamp stage to work
RUN apt-get update && \
  apt-get install -qy tzdata ca-certificates wget libsystemd-dev && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=build /src/loki/clients/cmd/promtail/promtail /usr/bin/promtail
COPY clients/cmd/promtail/promtail-docker-config.yaml /etc/promtail/config.yml
ENTRYPOINT ["/usr/bin/promtail"]
CMD ["-config.file=/etc/promtail/config.yml"]
