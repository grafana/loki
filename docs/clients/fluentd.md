# Fluentd

Loki has a [Fluentd](https://fluentd.org/) output plugin called
`fluent-plugin-grafana-loki` that enables shipping logs to a private Loki
instance or [Grafana Cloud](https://grafana.com/oss/loki).

The plugin offers two line formats and uses protobuf to send compressed data to
Loki.

Key features:

* `extra_labels`: Labels to be added to every line of a log file, useful for
  designating environments
* `label_keys`: Customizable list of keys for stream labels
* `line_format`: Format to use when flattening the record to a log line (`json`
    or `key_value`).

## Installation

```bash
$ gem install fluent-plugin-grafana-loki
```

## Usage

In your Fluentd configuration, use `@type loki`. Additional configuration is
optional. Default values look like this:

```
<match **>
  @type loki
  url "https://logs-us-west1.grafana.net"
  username "#{ENV['LOKI_USERNAME']}"
  password "#{ENV['LOKI_PASSWORD']}"
  extra_labels {"env":"dev"}
  flush_interval 10s
  flush_at_shutdown true
  buffer_chunk_limit 1m
</match>
```

### Multi-worker usage

Loki doesn't currently support out-of-order inserts - if you try to insert a log
entry with an earlier timestamp after a log entry with with identical labels but
a later timestamp, the insert will fail with the message
`HTTP status code: 500, message: rpc error: code = Unknown desc = Entry out of
order`. Therefore, in order to use this plugin in a multi-worker Fluentd setup,
you'll need to include the worker ID in the labels sent to Loki.

For example, using
[fluent-plugin-record-modifier](https://github.com/repeatedly/fluent-plugin-record-modifier):

```
<filter mytag>
    @type record_modifier
    <record>
        fluentd_worker "#{worker_id}"
    </record>
</filter>

<match mytag>
  @type loki
  # ...
  label_keys "fluentd_worker"
  # ...
</match>
```

## Docker Image

There is a Docker image `grafana/fluent-plugin-grafana-loki:master` which
contains default configuration files to git log information
a host's `/var/log` directory, and from the host's journald. To use it, you can set
the `LOKI_URL`, `LOKI_USERNAME`, and `LOKI_PASSWORD` environment variables
(`LOKI_USERNAME` and `LOKI_PASSWORD` can be left blank if Loki is not protected
behind an authenticating proxy).

An example Docker Swarm Compose configuration looks like:

```yaml
services:
  fluentd:
    image: grafana/fluent-plugin-grafana-loki:master
    command:
      - "fluentd"
      - "-v"
      - "-p"
      - "/fluentd/plugins"
    environment:
      LOKI_URL: http://loki:3100
      LOKI_USERNAME:
      LOKI_PASSWORD:
    deploy:
      mode: global
    configs:
      - source: loki_config
        target: /fluentd/etc/loki/loki.conf
    networks:
      - loki
    volumes:
      - host_logs:/var/log
      # Needed for journald log ingestion:
      - /etc/machine-id:/etc/machine-id
      - /dev/log:/dev/log
      - /var/run/systemd/journal/:/var/run/systemd/journal/
    logging:
      options:
         tag: infra.monitoring
```

## Configuration

### Proxy Support

Starting with version 0.8.0, this gem uses `excon`, which supports proxy with
environment variables - https://github.com/excon/excon#proxy-support

### `url`

The URL of the Loki server to send logs to. When sending data the publish path
(`/loki/api/v1/push`) will automatically be appended. By default the URL is set to
`https://logs-us-west1.grafana.net`, the URL of the Grafana Labs [hosted
Loki](https://grafana.com/loki) service.

### `username` / `password`

Specify a username and password if the Loki server requires authentication.
If using the Grafana Labs' hosted Loki, the username needs to be set to your
instanceId and the password should be a grafana.com API Key.

### `tenant`

Loki is a multi-tenant log storage platform and all requests sent must include a
tenant. For some installations (like Hosted Loki) the tenant will be set
automatically by an authenticating proxy. Otherwise you can define a tenant to
be passed through. The tenant can be any string value.

### output format

Loki is intended to index and group log streams using only a small set of
labels and is not intended for full-text indexing. When sending logs to Loki,
the majority of log message will be sent as a single log "line".

There are few configurations settings to control the output format:

- `extra_labels`: (default: nil) set of labels to include with every Loki
  stream. (e.g., `{"env":"dev", "datacenter": "dc1"}`)

- `remove_keys`: (default: nil) comma separated list of record keys to
  remove. All other keys will be placed into the log line.

- `label_keys`: (default: "job,instance") comma separated list of keys to use as
  stream labels.

- `line_format`: format to use when flattening the record to a log line. Valid
  values are `json` or `key_value`. If set to `json` the log line sent to Loki
  will be the fluentd record (excluding any keys extracted out as labels) dumped
  as json. If set to `key_value`, the log line will be each item in the record
  concatenated together (separated by a single space) in the format
  `<key>=<value>`.

- `drop_single_key`: if set to true, when the set of extracted `label_keys`
    after dropping with `remove_keys`, the log line sent to Loki will just be
    the value of the single remaining record key.

### Buffer options

`fluentd-plugin-loki` extends [Fluentd's builtin Output
plugin](https://docs.fluentd.org/v1.0/articles/output-plugin-overview) and uses
the `compat_parameters` plugin helper. It adds the following options:

```
buffer_type memory
flush_interval 10s
retry_limit 17
retry_wait 1.0
num_threads 1
```
