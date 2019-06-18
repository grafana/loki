FROM fluent/fluentd:v1.3.2-debian

USER root
WORKDIR /home/fluent
ENV PATH /fluentd/vendor/bundle/ruby/2.3.0/bin:$PATH
ENV GEM_PATH /fluentd/vendor/bundle/ruby/2.3.0
ENV GEM_HOME /fluentd/vendor/bundle/ruby/2.3.0
# skip runtime bundler installation
ENV FLUENTD_DISABLE_BUNDLER_INJECTION 1

COPY docker/Gemfile* /fluentd/
RUN buildDeps="sudo make gcc g++ libc-dev ruby-dev" \
       && apt-get update \
       && apt-get install -y --no-install-recommends \
       $buildDeps libsystemd0 net-tools libjemalloc1 \
       && gem install bundler --version 1.16.2 \
       && bundle config silence_root_warning true \
       && bundle install --gemfile=/fluentd/Gemfile --path=/fluentd/vendor/bundle \
       && sudo gem sources --clear-all \
       && SUDO_FORCE_REMOVE=yes \
       apt-get purge -y --auto-remove \
       -o APT::AutoRemove::RecommendsImportant=false \
       $buildDeps \
       && rm -rf /var/lib/apt/lists/* \
       /home/fluent/.gem/ruby/2.3.0/cache/*.gem \
       /tmp/* /var/tmp/* /usr/lib/ruby/gems/*/cache/*.gem

COPY docker/entrypoint.sh /fluentd/entrypoint.sh
COPY lib/fluent/plugin/out_loki.rb /fluentd/plugins/out_loki.rb
COPY docker/conf/ /fluentd/etc/loki/

ENV FLUENTD_CONF="/fluentd/etc/loki/fluentd.conf"
ENV FLUENTD_OPT=""

ENV LOKI_URL "https://logs-us-west1.grafana.net"

# See https://packages.debian.org/stretch/amd64/libjemalloc1/filelist
ENV LD_PRELOAD="/usr/lib/x86_64-linux-gnu/libjemalloc.so.1"

# Overwrite ENTRYPOINT to run fluentd as root for /var/log / /var/lib
ENTRYPOINT ["/fluentd/entrypoint.sh"]