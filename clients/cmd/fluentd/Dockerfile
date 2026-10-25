FROM ruby:4.0.7@sha256:cb7e8ade83823d88a8ed9d56da1df9fffe76bc1f5d2edaada3c79f84db92fcbc AS build

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    sudo make gcc g++ libc-dev ruby-dev golang

COPY . /src/loki
WORKDIR /src/loki
RUN make BUILD_IN_CONTAINER=false fluentd-plugin

FROM fluent/fluentd:v1.19-debian-1
ENV LOKI_URL="https://logs-prod-us-central1.grafana.net"

COPY --from=build /src/loki/clients/cmd/fluentd/lib/fluent/plugin/out_loki.rb /fluentd/plugins/out_loki.rb

COPY clients/cmd/fluentd/docker/Gemfile /fluentd/
COPY clients/cmd/fluentd/docker/conf/loki.conf /fluentd/etc/loki.conf

USER root
RUN sed -i '$i''  @include loki.conf' /fluentd/etc/fluent.conf
USER fluent
