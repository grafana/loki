---
title: Scraping
---
# Promtail Scraping (Service Discovery)

## File Target Discovery

Promtail discovers locations of log files and extract labels from them through
the `scrape_configs` section in the config YAML. The syntax is identical to what
[Prometheus uses](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config).

`scrape_configs` contains one or more entries which are executed for each
discovered target (i.e., each container in each new pod running in the
instance):

```
scrape_configs:
  - job_name: local
    static_configs:
      - ...

  - job_name: kubernetes
    kubernetes_sd_config:
      - ...
```

If more than one scrape config section matches your logs, you will get duplicate
entries as the logs are sent in different streams likely with slightly
different labels.

There are different types of labels present in Promtail:

* Labels starting with `__` (two underscores) are internal labels. They usually
  come from dynamic sources like service discovery. Once relabeling is done,
  they are removed from the label set. To persist internal labels so they're
  sent to Loki, rename them so they don't start with `__`. See
  [Relabeling](#relabeling) for more information.

* Labels starting with `__meta_kubernetes_pod_label_*` are "meta labels" which
  are generated based on your Kubernetes pod's labels.

  For example, if your Kubernetes pod has a label `name` set to `foobar`, then
  the `scrape_configs` section will receive an internal label
  `__meta_kubernetes_pod_label_name` with a value set to `foobar`.

* Other labels starting with `__meta_kubernetes_*` exist based on other
  Kubernetes metadata, such as the namespace of the pod
  (`__meta_kubernetes_namespace`) or the name of the container inside the pod
  (`__meta_kubernetes_pod_container_name`). Refer to
  [the Prometheus docs](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config)
  for the full list of Kubernetes meta labels.

* The `__path__` label is a special label which Promtail uses after discovery to
  figure out where the file to read is located. Wildcards are allowed, for example `/var/log/*.log` to get all files with a `log` extension in the specified directory, and `/var/log/**/*.log` for matching files and directories recursively. For a full list of options check out the [docs for the library promtail uses.](https://github.com/bmatcuk/doublestar)

* The label `filename` is added for every file found in `__path__` to ensure the
  uniqueness of the streams. It is set to the absolute path of the file the line
  was read from.

### Kubernetes Discovery

Note that while Promtail can utilize the Kubernetes API to discover pods as
targets, it can only read log files from pods that are running on the same node
as the one Promtail is running on. Promtail looks for a `__host__` label on
each target and validates that it is set to the same hostname as Promtail's
(using either `$HOSTNAME` or the hostname reported by the kernel if the
environment variable is not set).

This means that any time Kubernetes service discovery is used, there must be a
`relabel_config` that creates the intermediate label `__host__` from
`__meta_kubernetes_pod_node_name`:

```yaml
relabel_configs:
  - source_labels: ['__meta_kubernetes_pod_node_name']
    target_label: '__host__'
```

See [Relabeling](#relabeling) for more information.

## Journal Scraping (Linux Only)

On systems with `systemd`, Promtail also supports reading from the journal. Unlike
file scraping which is defined in the `static_configs` stanza, journal scraping is
defined in a `journal` stanza:

```yaml
scrape_configs:
  - job_name: journal
    journal:
      json: false
      max_age: 12h
      path: /var/log/journal
      labels:
        job: systemd-journal
    relabel_configs:
      - source_labels: ['__journal__systemd_unit']
        target_label: 'unit'
```

All fields defined in the `journal` section are optional, and are just provided
here for reference. The `max_age` field ensures that no older entry than the
time specified will be sent to Loki; this circumvents "entry too old" errors.
The `path` field tells Promtail where to read journal entries from. The labels
map defines a constant list of labels to add to every journal entry that Promtail
reads.

When the `json` field is set to `true`, messages from the journal will be
passed through the pipeline as JSON, keeping all of the original fields from the
journal entry. This is useful when you don't want to index some fields but you
still want to know what values they contained.

By default, Promtail reads from the journal by looking in the `/var/log/journal`
and `/run/log/journal` paths. If running Promtail inside of a Docker container,
the path appropriate to your distribution should be bind mounted inside of
Promtail along with binding `/etc/machine-id`. Bind mounting `/etc/machine-id`
to the path of the same name is required for the journal reader to know which
specific journal to read from. For example:

```bash
docker run \
  -v /var/log/journal/:/var/log/journal/ \
  -v /run/log/journal/:/run/log/journal/ \
  -v /etc/machine-id:/etc/machine-id \
  grafana/promtail:latest \
  -config.file=/path/to/config/file.yaml
```

When Promtail reads from the journal, it brings in all fields prefixed with
`__journal_` as internal labels. Like in the example above, the `_SYSTEMD_UNIT`
field from the journal was transformed into a label called `unit` through
`relabel_configs`. See [Relabeling](#relabeling) for more information, also look at [the systemd man pages](https://www.freedesktop.org/software/systemd/man/systemd.journal-fields.html) for a list of fields exposed by the journal.

Here's an example where the `SYSTEMD_UNIT`, `HOSTNAME`, and `SYSLOG_IDENTIFIER` are relabeled for use in Loki.

Keep in mind that labels prefixed with `__` will be dropped, so relabeling is required to keep these labels.

```yaml
- job_name: systemd-journal
  journal:
    labels:
      cluster: ops-tools1
      job: default/systemd-journal
    path: /var/log/journal
  relabel_configs:
  - source_labels:
    - __journal__systemd_unit
    target_label: systemd_unit
  - source_labels:
    - __journal__hostname
    target_label: nodename
  - source_labels:
    - __journal_syslog_identifier
    target_label: syslog_identifier
```

## Syslog Receiver

Promtail supports receiving [IETF Syslog (RFC5424)](https://tools.ietf.org/html/rfc5424)
messages from a tcp stream. Receiving syslog messages is defined in a `syslog`
stanza:

```yaml
scrape_configs:
  - job_name: syslog
    syslog:
      listen_address: 0.0.0.0:1514
      idle_timeout: 60s
      label_structured_data: yes
      labels:
        job: "syslog"
    relabel_configs:
      - source_labels: ['__syslog_message_hostname']
        target_label: 'host'
```

The only required field in the syslog section is the `listen_address` field,
where a valid network address should be provided. The `idle_timeout` can help
with cleaning up stale syslog connections. If `label_structured_data` is set,
[structured data](https://tools.ietf.org/html/rfc5424#section-6.3) in the
syslog header will be translated to internal labels in the form of
`__syslog_message_sd_<ID>_<KEY>`.
The labels map defines a constant list of labels to add to every journal entry
that Promtail reads.

Note that it is recommended to deploy a dedicated syslog forwarder
like **syslog-ng** or **rsyslog** in front of Promtail.
The forwarder can take care of the various specifications
and transports that exist (UDP, BSD syslog, ...). See recommended output
configurations for [syslog-ng](#syslog-ng-output-configuration) and
[rsyslog](#rsyslog-output-configuration).

When Promtail receives syslog messages, it brings in all header fields,
parsed from the received message, prefixed with `__syslog_` as internal labels.
Like in the example above, the `__syslog_message_hostname`
field from the journal was transformed into a label called `host` through
`relabel_configs`. See [Relabeling](#relabeling) for more information.

### Syslog-NG Output Configuration

```
destination d_loki {
  syslog("localhost" transport("tcp") port(<promtail_port>));
};
```

### Rsyslog Output Configuration

```
action(type="omfwd" protocol="tcp" port="<promtail_port>" Template="RSYSLOG_SyslogProtocol23Format" TCP_Framing="octet-counted")
```

## Relabeling

Each `scrape_configs` entry can contain a `relabel_configs` stanza.
`relabel_configs` is a list of operations to transform the labels from discovery
into another form.

A single entry in `relabel_configs` can also reject targets by doing an `action:
drop` if a label value matches a specified regex. When a target is dropped, the
owning `scrape_config` will not process logs from that particular source.
Other `scrape_configs` without the drop action reading from the same target
may still use and forward logs from it to Loki.

A common use case of `relabel_configs` is to transform an internal label such
as `__meta_kubernetes_*` into an intermediate internal label such as
`__service__`. The intermediate internal label may then be dropped based on
value or transformed to a final external label, such as `__job__`.

### Examples

* Drop the target if a label (`__service__` in the example) is empty:
```yaml
  - action: drop
    regex: ''
    source_labels:
    - __service__
```
* Drop the target if any of the `source_labels` contain a value:
```yaml
  - action: drop
    regex: .+
    separator: ''
    source_labels:
    - __meta_kubernetes_pod_label_name
    - __meta_kubernetes_pod_label_app
```
* Persist an internal label by renaming it so it will be sent to Loki:
```yaml
  - action: replace
    source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
```
* Persist all Kubernetes pod labels by mapping them, like by mapping
    `__meta_kube__meta_kubernetes_pod_label_foo` to `foo`.
```yaml
  - action: labelmap
    regex: __meta_kubernetes_pod_label_(.+)
```

Additional reading:

 * [Julien Pivotto's slides from PromConf Munich, 2017](https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749)

## HTTP client options

Promtail uses the Prometheus HTTP client implementation for all calls to Loki.
Therefore it can be configured using the `clients` stanza, where one or more
connections to Loki can be established:

```yaml
clients:
  - [ <client_option> ]
```

Refer to [`client_config`](../configuration#client_config) from the Promtail
Configuration reference for all available options.
