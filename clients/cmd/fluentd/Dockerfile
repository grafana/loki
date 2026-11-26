FROM ruby:4.0.10@sha256:84e854507d7f2ffc9061e32d47c16d7413bcb1fa55ec29c1927f9e4d62fa6cf3 AS build

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    sudo make gcc g++ libc-dev ruby-dev golang

COPY . /src/loki
WORKDIR /src/loki
RUN make BUILD_IN_CONTAINER=false fluentd-plugin

FROM fluent/fluentd:v1.20-debian-1@sha256:b3712e2ad2530f61dfe770390ac3f046df761641dc0b380f14de3c588796527f
ENV LOKI_URL="https://logs-prod-us-central1.grafana.net"

COPY --from=build /src/loki/clients/cmd/fluentd/lib/fluent/plugin/out_loki.rb /fluentd/plugins/out_loki.rb

COPY clients/cmd/fluentd/docker/Gemfile /fluentd/
COPY clients/cmd/fluentd/docker/conf/loki.conf /fluentd/etc/loki.conf

USER root
RUN sed -i '$i''  @include loki.conf' /fluentd/etc/fluent.conf
USER fluent
