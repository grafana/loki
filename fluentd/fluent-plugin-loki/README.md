# fluent-plugin-loki

[Fluentd](https://fluentd.org/) output plugin to ship logs to a Loki server.

This plugin offers two line formats and uses protobuf to send compressed data to Loki.

Key features:
  * extra_labels - labels to be added to every line of a logfile, useful for designating environments
  * label_keys - customizable list of keys for stream labels

## Installation

```
$ gem install fluent-plugin-loki
```

## Usage
In your Fluentd configuration, use `@type loki`. Additional configuration is optional, default values would look like this:
```
<match **>
  @type loki
  url "https://logs-us-west1.grafana.net"
  username "#{ENV['LOKI_USERNAME']}"
  password "#{ENV['LOKI_PASSWORD']}"
  labels {"env":"dev"}
  flush_interval 10s
  flush_at_shutdown true
  buffer_chunk_limit 1m
</match>
```


## Configuration

### url
The url of the Loki server to send logs to.  When sending data the publish path (`/api/prom/push`) will automatically be appended.
By default the url is set to `https://logs-us-west1.grafana.net`, the url of the Grafana Labs preview (hosted Loki)[https://grafana.com/loki] service.

#### Proxy Support

Starting with version 0.8.0, this gem uses excon, which supports proxy with environment variables - https://github.com/excon/excon#proxy-support

### username / password
Specify a username and password if the Loki server requires authentication. 
If using the GrafanaLab's hosted Loki, the username needs to be set to your instanceId and the password should be a Grafana.com api key.

### tenant
Loki is a multi-tenant log storage platform and all requests sent must include a tenant.  For some installations the tenant will be set automatically by an authenticating proxy.  Otherwise you can define a tenant to be passed through.  The tenant can be any string value.


### output format
Loki is intended to index and group log streams using only a small set of labels.  It is not intended for full-text indexing.  When sending logs to Loki the majority of log message will be sent as a single log "line".  

There are 3 configurations settings to control the output format.
 - extra_labels: (default: nil) set of labels to include with every Loki stream. eg `{"env":"dev", "datacenter": "dc1"}`
 - label_keys: (default: "job,instance") comma separated list of keys to use as stream labels.  All other keys will be placed into the log line
 - line_format: format to use when flattening the record to a log line. Valid values are "json" or "key_value".  If set to "json" the log line sent to Loki will be the fluentd record (excluding any keys extracted out as labels) dumped as json.  If set to "key_value", the log line will be each item in the record concatenated together (separated by a single space) in the format `<key>=<value>`.
 - drop_single_key: if set to true and after extracting label_keys a record only has a single key remaining, the log line sent to Loki will just be the value of the record key.

### Buffer options

`fluentd-plugin-loki` extends [Fluentd's builtin Output plugin](https://docs.fluentd.org/v1.0/articles/output-plugin-overview) and use `compat_parameters` plugin helper. It adds the following options:

```
buffer_type memory
flush_interval 10s
retry_limit 17
retry_wait 1.0
num_threads 1
```

## Development

After checking out the repo, run `bin/setup` to install dependencies. Then, run `rake spec` to run the tests. You can also run `bin/console` for an interactive prompt that will allow you to experiment.

To install this gem onto your local machine, run `bundle exec rake install`. To release a new version, update the version number in `fluent-plugin-loki.gemspec`, and then run `bundle exec rake release`, which will create a git tag for the version, push git commits and tags, and push the `.gem` file to [rubygems.org](https://rubygems.org).

To create the gem: `gem build fluent-plugin-loki.gemspec`

Useful additions:
  `gem install rubocop`

## Copyright

* Copyright(c) 2018- Grafana Labs
* License
  * Apache License, Version 2.0
