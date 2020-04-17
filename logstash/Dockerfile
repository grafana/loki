FROM logstash:7.6.2

ARG PLUGIN_VERSION=1.0.0
USER logstash

COPY --chown=logstash:logstash . /home/logstash/
WORKDIR /home/logstash/

ENV PATH /usr/share/logstash/vendor/jruby/bin:/usr/share/logstash/vendor/bundle/jruby/2.5.0/bin:$PATH
ENV LOGSTASH_SOURCE="1"
ENV LOGSTASH_PATH="/usr/share/logstash"
ENV GEM_PATH /usr/share/logstash/vendor/bundle/jruby/2.5.0
ENV GEM_HOME /usr/share/logstash/vendor/bundle/jruby/2.5.0

RUN gem install bundler:2.1.4

RUN bundle install --path=/usr/share/logstash/vendor/bundle && \
    bundle exec rake vendor && \
    bundle exec rspec

RUN gem build logstash-output-grafana-loki.gemspec && \
    /usr/share/logstash/bin/logstash-plugin install logstash-output-grafana-loki-${PLUGIN_VERSION}.gem

EXPOSE 5044

CMD ["/usr/share/logstash/bin/logstash", "-f", "loki.conf"]
