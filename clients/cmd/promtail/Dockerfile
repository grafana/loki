FROM golang:1.17.9-bullseye@sha256:a4bd6256540bb5283b13dbc8aa9fdfd3440b009ce65771d293a97f1da6af5955 as build

COPY . /src/loki
WORKDIR /src/loki
# Backports repo required to get a libsystemd version 246 or newer which is required to handle journal +ZSTD compression
RUN echo "deb http://deb.debian.org/debian bullseye-backports main" >> /etc/apt/sources.list
RUN apt-get update && apt-get install -t bullseye-backports -qy libsystemd-dev
RUN make clean && make BUILD_IN_CONTAINER=false promtail

# Promtail requires debian as the base image to support systemd journal reading
FROM debian:bullseye-slim@sha256:9b42b2e7eddd84eaddb67b45567cdc0e03ec826bf252352f300147cfb8ce5a6d
# tzdata required for the timestamp stage to work
# Backports repo required to get a libsystemd version 246 or newer which is required to handle journal +ZSTD compression
RUN echo "deb http://deb.debian.org/debian bullseye-backports main" >> /etc/apt/sources.list
RUN apt-get update && \
  apt-get install -qy \
  tzdata ca-certificates
RUN apt-get install -t bullseye-backports -qy libsystemd-dev && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=build /src/loki/clients/cmd/promtail/promtail /usr/bin/promtail
COPY clients/cmd/promtail/promtail-docker-config.yaml /etc/promtail/config.yml
ENTRYPOINT ["/usr/bin/promtail"]
CMD ["-config.file=/etc/promtail/config.yml"]
