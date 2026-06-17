FROM logstash:9.4.2@sha256:648a781b7360736754a9f583f7819d166be9dcee42d272bbce7d67278f529398

USER logstash
ENV PATH /usr/share/logstash/vendor/jruby/bin:/usr/share/logstash/vendor/bundle/jruby/3.1.0/bin:/usr/share/logstash/jdk/bin:$PATH
ENV LOGSTASH_PATH /usr/share/logstash
ENV GEM_PATH /usr/share/logstash/vendor/bundle/jruby/3.1.0
ENV GEM_HOME /usr/share/logstash/vendor/bundle/jruby/3.1.0

RUN gem install bundler -v 2.6.9

COPY --chown=logstash:logstash ./clients/cmd/logstash/ /home/logstash/
WORKDIR /home/logstash/

# don't run 'bundle update'. It causes a transitive dependency error
RUN bundle config set --local path /usr/share/logstash/vendor/bundle && \
    bundle install && \
    bundle exec rake vendor && \
    bundle exec rspec

RUN cat logstash-output-loki.gemspec | grep s.version | awk '{print $3}' |  cut -d "'" -f 2 > VERSION

RUN gem build logstash-output-loki.gemspec && \
    PLUGIN_VERSION=$(cat VERSION); /usr/share/logstash/bin/logstash-plugin install logstash-output-loki-${PLUGIN_VERSION}.gem

EXPOSE 5044
