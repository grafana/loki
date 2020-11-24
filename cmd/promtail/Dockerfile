FROM golang:1.15.3-buster as build
# TOUCH_PROTOS signifies if we should touch the compiled proto files and thus not regenerate them.
# This is helpful when file system timestamps can't be trusted with make
ARG TOUCH_PROTOS
COPY . /src/loki
WORKDIR /src/loki
# Backports repo required to get a libsystemd version 246 or newer which is required to handle journal +ZSTD compression
RUN echo "deb http://deb.debian.org/debian buster-backports main" >> /etc/apt/sources.list
RUN apt-get update && apt-get install -t buster-backports -qy libsystemd-dev
RUN make clean && (if [ "${TOUCH_PROTOS}" ]; then make touch-protos; fi) && make BUILD_IN_CONTAINER=false promtail

# Promtail requires debian as the base image to support systemd journal reading
FROM debian:buster-slim
# tzdata required for the timestamp stage to work
# Backports repo required to get a libsystemd version 246 or newer which is required to handle journal +ZSTD compression
RUN echo "deb http://deb.debian.org/debian buster-backports main" >> /etc/apt/sources.list
RUN apt-get update && \
  apt-get install -qy \
  tzdata ca-certificates
RUN apt-get install -t buster-backports -qy libsystemd-dev && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=build /src/loki/cmd/promtail/promtail /usr/bin/promtail
COPY cmd/promtail/promtail-docker-config.yaml /etc/promtail/config.yml
ENTRYPOINT ["/usr/bin/promtail"]
CMD ["-config.file=/etc/promtail/config.yml"]
