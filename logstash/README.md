# Loki Logstash Output Plugin
Logstash plugin to send logstash aggregated logs to Loki.


## Install dependencies
First you need to setup JRuby environment to build this plugin. Refer https://github.com/rbenv/rbenv for setting up your rbenv environment.

After setting up `rbenv`. Install JRuby
```
rbenv install jruby-9.2.10.0
rbenv local jruby-9.2.10.0
```
Check that the environment is configured
```
ruby --version
jruby 9.2.10
```

Then install bundler
`gem install bundler -v '< 2'`

Make sure you have logstash installed before going to following section

## Install dependencies and Build plugin

### Install required packages
```
bundle install --path=/path/to/logstash/vendor/bundle
bundle exec rake vendor
```

### Build the plugin
`gem build logstash-output-grafana-loki.gemspec`

### Test
`bundle exec rspec`

## Install plugin to local logstash
`bin/logstash-plugin install --no-verify --local logstash-output-grafana-loki-1.0.0.gem`

## Send sample event and check plugin is working
`bin/logstash -f loki.conf`