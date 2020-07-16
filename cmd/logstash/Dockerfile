FROM logstash:7.6.2

USER logstash
ENV PATH /usr/share/logstash/vendor/jruby/bin:/usr/share/logstash/vendor/bundle/jruby/2.5.0/bin:$PATH
ENV LOGSTASH_PATH="/usr/share/logstash"
ENV GEM_PATH /usr/share/logstash/vendor/bundle/jruby/2.5.0
ENV GEM_HOME /usr/share/logstash/vendor/bundle/jruby/2.5.0

RUN gem install bundler:2.1.4

COPY --chown=logstash:logstash ./cmd/logstash/ /home/logstash/
WORKDIR /home/logstash/


RUN bundle install --path=/usr/share/logstash/vendor/bundle && \
    bundle exec rake vendor && \
    bundle exec rspec

RUN cat logstash-output-loki.gemspec | grep s.version | awk '{print $3}' |  cut -d "'" -f 2 > VERSION

RUN gem build logstash-output-loki.gemspec && \
    PLUGIN_VERSION=$(cat VERSION); /usr/share/logstash/bin/logstash-plugin install logstash-output-loki-${PLUGIN_VERSION}.gem

EXPOSE 5044
