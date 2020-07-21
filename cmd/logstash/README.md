# Contributing to Loki Logstash Output Plugin

For information about hwo to use this plugin see this [documentation](../../docs/clients/logstash/README.md).

## Install dependencies

First you need to setup JRuby environment to build this plugin. Refer https://github.com/rbenv/rbenv for setting up your rbenv environment.

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

You should use make sure you are running jruby and not ruby. If the command below still shows ruby and not jruby, check that PATH contains `$HOME/.rbenv/shims` and `$HOME/.rbenv/bin`. Also verify that you have this in your bash profile:

```bash
export PATH="$HOME/.rbenv/bin:$PATH"
eval "$(rbenv init -)"
```

Then install bundler
`gem install bundler:2.1.4`

Follow those instructions to [install logstash](https://www.elastic.co/guide/en/logstash/current/installing-logstash.html) before moving to the next section.

## Install dependencies and Build plugin

### Install required packages

```bash
git clone git@github.com:elastic/logstash.git
cd logstash
git checkout tags/v7.6.2
export LOGSTASH_PATH=`pwd`
export GEM_PATH=$LOGSTASH_PATH/vendor/bundle/jruby/2.5.0
export GEM_HOME=$LOGSTASH_PATH/vendor/bundle/jruby/2.5.0
./gradlew assemble
cd ..
ruby -S bundle install
ruby -S bundle exec rake vendor
```

### Build the plugin

`gem build logstash-output-loki.gemspec`

### Test

`ruby -S bundle exec rspec`

Alternatively if you don't want to install JRuby. Enter inside logstash-loki container.

```bash
docker build -t logstash-loki ./
docker run -v  `pwd`/spec:/home/logstash/spec -it --rm --entrypoint /bin/sh logstash-loki
bundle exec rspec
```

## Install plugin to local logstash

`bin/logstash-plugin install --no-verify --local logstash-output-loki-1.0.0.gem`

## Send sample event and check plugin is working

`bin/logstash -f loki.conf`
