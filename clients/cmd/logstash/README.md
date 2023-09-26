# Contributing to Loki Logstash Output Plugin

For information about how to use this plugin see this [documentation](../../docs/sources/clients/logstash/_index.md).

## Install dependencies

First, make sure you have JDK version `8` or `11` installed and you have set the `JAVA_HOME` environment variable.

You need to setup JRuby environment to build this plugin. Refer https://github.com/rbenv/rbenv for setting up your rbenv environment.

After setting up `rbenv`. Install JRuby

```bash
rbenv install jruby-9.2.10.0
rbenv local jruby-9.2.10.0
```

Check that the environment is configured

```bash
ruby --version
jruby 9.2.10
```

You should make sure you are running `jruby` and not `ruby`. If the command `ruby --version` still shows `ruby` and not `jruby`, check that PATH contains `$HOME/.rbenv/shims` and `$HOME/.rbenv/bin`. Also verify that you have this in your bash profile:

```bash
export PATH="$HOME/.rbenv/bin:$PATH"
eval "$(rbenv init -)"
```

Then install bundler:

```bash
gem install bundler:2.1.4
```

Follow those instructions to [install logstash](https://www.elastic.co/guide/en/logstash/current/installing-logstash.html) before moving to the next section.

## Build and test the plugin

### Install required packages

```bash
git clone git@github.com:elastic/logstash.git
cd logstash
git checkout tags/v7.16.1
export LOGSTASH_PATH="$(pwd)"
export GEM_PATH="$LOGSTASH_PATH/vendor/bundle/jruby/2.5.0"
export GEM_HOME="$LOGSTASH_PATH/vendor/bundle/jruby/2.5.0"
./gradlew assemble
cd ..
ruby -S bundle config set --local path "$LOGSTASH_PATH/vendor/bundle"
ruby -S bundle install
ruby -S bundle exec rake vendor
```

### Build the plugin

```bash
gem build logstash-output-loki.gemspec
```

### Test

```bash
ruby -S bundle exec rspec
```

Alternatively if you don't want to install JRuby. Enter inside logstash-loki container.

```bash
docker build -t logstash-loki ./
docker run -v  $(pwd)/spec:/home/logstash/spec -it --rm --entrypoint /bin/sh logstash-loki
bundle exec rspec
```

## Install plugin to local logstash

```bash
bin/logstash-plugin install --no-verify --local logstash-output-loki-1.0.0.gem
```

## Send sample event and check plugin is working

```bash
bin/logstash -f loki.conf
```
