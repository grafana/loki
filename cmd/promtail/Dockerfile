FROM golang:1.13 as build
COPY . /src/loki
WORKDIR /src/loki
RUN apt-get update && apt-get install -qy libsystemd-dev
RUN make clean && make BUILD_IN_CONTAINER=false promtail

# Promtail requires debian as the base image to support systemd journal reading
FROM debian:stretch-slim
# tzdata required for the timestamp stage to work
RUN apt-get update && \
  apt-get install -qy \
  tzdata ca-certificates libsystemd-dev && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=build /src/loki/cmd/promtail/promtail /usr/bin/promtail
COPY cmd/promtail/promtail-local-config.yaml /etc/promtail/local-config.yaml
COPY cmd/promtail/promtail-docker-config.yaml /etc/promtail/docker-config.yaml
ENTRYPOINT ["/usr/bin/promtail"]
