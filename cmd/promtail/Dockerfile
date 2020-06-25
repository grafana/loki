FROM golang:1.14.2 as build
# TOUCH_PROTOS signifies if we should touch the compiled proto files and thus not regenerate them.
# This is helpful when file system timestamps can't be trusted with make
ARG TOUCH_PROTOS
COPY . /src/loki
WORKDIR /src/loki
RUN apt-get update && apt-get install -qy libsystemd-dev
RUN make clean && (if [ "${TOUCH_PROTOS}" ]; then make touch-protos; fi) && make BUILD_IN_CONTAINER=false promtail

# Promtail requires debian as the base image to support systemd journal reading
FROM debian:stretch-slim
# tzdata required for the timestamp stage to work
RUN apt-get update && \
  apt-get install -qy \
  tzdata ca-certificates libsystemd-dev && \
  rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
COPY --from=build /src/loki/cmd/promtail/promtail /usr/bin/promtail
COPY cmd/promtail/promtail-docker-config.yaml /etc/promtail/config.yml
ENTRYPOINT ["/usr/bin/promtail"]
CMD ["-config.file=/etc/promtail/config.yml"]
