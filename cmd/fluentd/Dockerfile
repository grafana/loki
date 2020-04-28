FROM ruby:2.6 as build

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    sudo make gcc g++ libc-dev ruby-dev golang

COPY . /src/loki
WORKDIR /src/loki
RUN make BUILD_IN_CONTAINER=false fluentd-plugin

FROM fluent/fluentd:v1.9.2-debian-1.0
COPY --from=build /src/loki/cmd/fluentd/lib/fluent/plugin/out_loki.rb /fluentd/plugins/out_loki.rb

## skip runtime bundler installation
ENV FLUENTD_DISABLE_BUNDLER_INJECTION 1
ENV DEBIAN_FRONTEND=noninteractive

USER root
RUN apt-get update && apt-get install -y --no-install-recommends libjemalloc2

COPY cmd/fluentd/docker/Gemfile* /fluentd/
COPY cmd/fluentd/docker/entrypoint.sh /fluentd/entrypoint.sh
COPY cmd/fluentd/docker/conf/ /fluentd/etc/loki/

ENV FLUENTD_CONF="/fluentd/etc/loki/fluentd.conf"
ENV FLUENTD_OPT=""

ENV LOKI_URL "https://logs-us-west1.grafana.net"

# See https://packages.debian.org/stretch/amd64/libjemalloc1/filelist
ENV LD_PRELOAD="/usr/lib/x86_64-linux-gnu/libjemalloc.so.2"

# Overwrite ENTRYPOINT to run fluentd as root for /var/log / /var/lib
ENTRYPOINT ["/fluentd/entrypoint.sh"]
