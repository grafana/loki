---
title: Fluentd client
menuTitle:  Fluentd
description: Instructions for how to install, configure, and use the Fluentd client to send logs to Loki.
aliases: 
- ../clients/fluentd/
weight:  600
---
# Fluentd client

The [Fluentd output plugin](https://www.fluentd.org/) for Grafana Loki is called
`fluent-plugin-grafana-loki` that enables shipping logs to a private Loki
instance or [Grafana Cloud](/products/cloud/).

The plugin source code is in the [fluentd directory of the repository](https://github.com/grafana/loki/tree/main/clients/cmd/fluentd).

{{< youtube id="s43IBSVyTpQ" >}}

## Installation

### Local

To install the plugin use [fluent-gem](https://docs.fluentd.org/deployment/plugin-management):

```bash
fluent-gem install fluent-plugin-grafana-loki
```

## Docker Image

The Docker image `grafana/fluent-plugin-loki:main` contains [default configuration files](https://github.com/grafana/loki/tree/main/clients/cmd/fluentd/docker/conf). By default, fluentd containers use that default configuration. You can instead specify your `fluentd.conf` configuration file with a `FLUENTD_CONF` environment variable.

This image also uses `LOKI_URL`, `LOKI_USERNAME`, and `LOKI_PASSWORD` environment variables to specify the Loki endpoint, user, and password (you can leave the USERNAME and PASSWORD blank if they're not used).

This image starts an instance of Fluentd that forwards incoming logs to the specified Loki URL. As an alternate, containerized applications can also use [docker driver plugin]({{< relref "../docker-driver" >}}) to ship logs without needing Fluentd.

### Example

A Docker Compose configuration that will work looks like:

```yaml
services:
  fluentd:
    image: grafana/fluent-plugin-loki:main
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

## Usage

{{% admonition type="note" %}}
Use either `<label>...</label>` or `extra_labels` to set at least one label.
{{% /admonition %}}

In your Fluentd configuration, add `@type loki`. Additional configuration is optional. Default values would look like this:

```conf
<match **>
  @type loki
  url "https://logs-prod-us-central1.grafana.net"
  username "#{ENV['LOKI_USERNAME']}"
  password "#{ENV['LOKI_PASSWORD']}"
  extra_labels {"env":"dev"}
  <buffer>
    flush_interval 10s
    flush_at_shutdown true
  </buffer>
  buffer_chunk_limit 1m
</match>
```

### Adding labels

Simple label from top level attribute

```conf
<match mytag>
  @type loki
  # ...
  <label>
    fluentd_worker
  </label>
  # ...
</match>
```

You can rewrite the label keys as well as the following

```conf
<match mytag>
  @type loki
  # ...
  <label>
    worker fluentd_worker
  </label>
  # ...
</match>
```

You can use [record accessor](https://docs.fluentd.org/plugin-helper-overview/api-plugin-helper-record_accessor#syntax) syntax for nested field.

```conf
<match mytag>
  @type loki
  # ...
  <label>
    container $.kubernetes.container
  </label>
  # ...
</match>
```

### Extracting Kubernetes labels

Since Kubernetes labels are a list of nested key-value pairs, a separate option is available to extract them.
Note that special characters like "`. - /`" will be overwritten with `_`.
Use with the `remove_keys kubernetes` option to eliminate metadata from the log.

```conf
<match mytag>
  @type loki
  # ...
  extract_kubernetes_labels true
  remove_keys kubernetes
  <label>
    container $.kubernetes.container
  </label>
  # ...
</match>
```

> You can also include automatically all kubernetes labels by using `extract_kubernetes_labels true` in your configuration.

### Multi-worker usage

Loki enables out-of-order inserts by default; refer to [accept out-of-order writes](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#accept-out-of-order-writes).
If out-of-order inserts are _disabled_, attempting to insert a log entry with an earlier timestamp after a log entry with identical labels but a later timestamp, the insert will fail with `HTTP status code: 500, message: rpc error: code = Unknown desc = Entry out of order`. Therefore, in order to use this plugin in a multi worker Fluentd setup, you'll need to include the worker ID in the labels or otherwise [ensure log streams are always sent to the same worker](https://docs.fluentd.org/deployment/multi-process-workers#less-than-worker-n-greater-than-directive).

For example, using [fluent-plugin-record-modifier](https://github.com/repeatedly/fluent-plugin-record-modifier):

```conf
<filter mytag>
    @type record_modifier
    <record>
        fluentd_worker "#{worker_id}"
    </record>
</filter>

<match mytag>
  @type loki
  # ...
  <label>
    fluentd_worker
  </label>
  # ...
</match>
```

### Using multiple buffer flush threads

Similarly, when using `flush_thread_count` > 1 in the [`buffer`](https://docs.fluentd.org/configuration/buffer-section#flushing-parameters)
section, a thread identifier must be added as a label to ensure that log chunks flushed in parallel to loki by fluentd always have increasing
times for their unique label sets.

This plugin automatically adds a `fluentd_thread` label with the name of the buffer flush thread when `flush_thread_count` > 1.

## Configuration

### `url`

The URL of the Loki server to send logs to. When sending data, the publish path (`../reference/api/loki-http-api/v1/push/`) will automatically be appended.
By default the url is set to `https://logs-prod-us-central1.grafana.net`, the url of the Grafana Labs [hosted Loki](/products/cloud/) service.

#### Proxy Support

Starting with version 0.8.0, this gem uses [excon, which supports proxy with environment variables](https://github.com/excon/excon#proxy-support).

### `username` / `password`

If the Loki server requires authentication, specify a username and password.
If using the GrafanaLab's hosted Loki, the username needs to be set to your instanceId and the password should be a Grafana.com api key.

### tenant

All requests sent to Loki, a multi-tenant log storage platform, must include a tenant. For some installations the tenant will be set automatically by an authenticating proxy. Otherwise you can define a tenant to be passed through.
The tenant can be any string value.

The tenant field also supports placeholders, allowing it to dynamically change based on tag and record fields. Each placeholder must be added as a buffer chunk key. The following is an example of setting the tenant based on a Kubernetes pod label:

```conf
<match **>
  @type loki
  url "https://logs-prod-us-central1.grafana.net"
  tenant ${$.kubernetes.labels.tenant}
  # ...
  <buffer $.kubernetes.labels.tenant>
    @type memory
    flush_interval 5s
  </buffer>
</match>
```

### Client certificate verification

If a reverse proxy with client certificate verification is configured in front of Loki, specify a pair of client certificate and private key with `cert` and `key`. `ca_cert` can also be specified if the server uses custom certificate authority.

```conf
<match **>
  @type loki

  url "https://loki"

  cert /path/to/certificate.pem
  key /path/to/key.key
  ca_cert /path/to/ca.pem

  ...
</match>
```

### Server certificate verification

A flag to disable server certificate verification. By default the `insecure_tls` is set to false.

```conf
<match **>
  @type loki

  url "https://loki"

  insecure_tls true

  ...
</match>
```

### Output format

Loki is intended to index and group log streams using only a small set of labels. It is not intended for full-text indexing. When sending logs to Loki the majority of log message will be sent as a single log "line".

Several configuration settings are available to control the output format.

- extra_labels: (default: nil) set of labels to include with every Loki stream. eg `{"env":"dev", "datacenter": "dc1"}`
- remove_keys: (default: nil) comma separated list of needless record keys to remove. All other keys will be placed into the log line. You can use [record_accessor syntax](https://docs.fluentd.org/plugin-helper-overview/api-plugin-helper-record_accessor#syntax).
- line_format (default:key_value): format to use when flattening the record to a log line. Valid values are "json" or "key_value". If set to "json" the log line sent to Loki will be the fluentd record (excluding any keys extracted out as labels) dumped as json. If set to "key_value", the log line will be each item in the record concatenated together (separated by a single space) in the format `<key>=<value>`.
- drop_single_key: if set to true and a record only has 1 key after extracting `<label></label>` blocks, set the log line to the value and discard the key.
- include_thread_label (default: true): whether or not to include the fluentd_thread label when multiple threads are used for flushing.

### Buffer options

`fluentd-plugin-loki` extends [Fluentd's builtin Output plugin](https://docs.fluentd.org/v1.0/articles/output-plugin-overview) and use `compat_parameters` plugin helper. It adds the following options:

```conf
buffer_type memory
flush_interval 10s
retry_limit 17
retry_wait 1.0
num_threads 1
```
