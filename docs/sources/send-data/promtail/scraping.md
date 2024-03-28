---
title: Configuring Promtail for service discovery
menuTitle:  Configure service discovery
description: Configuring Promtail for service discovery
aliases: 
- ../../clients/promtail/scraping/
weight:  400
---

# Configuring Promtail for service discovery

Promtail currently supports scraping from the following sources:

- [Azure event hubs]({{< relref "#azure-event-hubs" >}})
- [Cloudflare]({{< relref "#cloudflare" >}})
- [File target discovery]({{< relref "#file-target-discovery" >}})
- [GCP Logs]({{< relref "#gcp-log-scraping" >}})
- [GELF]({{< relref "#gelf" >}})
- [Heroku Drain]({{< relref "#gcp-log-scraping" >}})
- [HTTP client]({{< relref "#http-client" >}})
- [journal scraping]({{< relref "#journal-scraping-linux-only" >}}) 
- [Kafka]({{< relref "#kafka" >}})
- [Relabeling]({{< relref "#relabeling" >}})
- [Syslog]({{< relref "#syslog-receiver" >}})
- [Windows]({{< relref "#windows-event-log" >}})

## Azure Event Hubs

Promtail supports reading messages from Azure Event Hubs.
Targets can be configured using the `azure_event_hubs` stanza:

```yaml
- job_name: azure_event_hubs
  azure_event_hubs:
    group_id: "mygroup"
    fully_qualified_namespace: my-namespace.servicebus.windows.net:9093
    connection_string: "my-connection-string"
    event_hubs:
      - event-hub-name
    labels:
      job: azure_event_hub
  relabel_configs:
    - action: replace
      source_labels:
        - __azure_event_hubs_category
      target_label: category
```

Only `fully_qualified_namespace`, `connection_string` and `event_hubs` are required fields.
Read the [configuration]({{< relref "./configuration#azure-event-hubs" >}}) section for more information.

## Cloudflare

Promtail supports pulling HTTP log messages from Cloudflare using the [Logpull API](https://developers.cloudflare.com/logs/logpull).
The Cloudflare targets can be configured with a `cloudflare` block:

```yaml
scrape_configs:
- job_name: cloudflare
  cloudflare:
    api_token: REDACTED
    zone_id: REDACTED
    fields_type: all
    labels:
      job: cloudflare-foo.com
```

Only `api_token` and `zone_id` are required.
Refer to the [Cloudfare]({{< relref "./configuration#cloudflare" >}}) configuration section for details.

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

- Labels starting with `__` (two underscores) are internal labels. They usually
  come from dynamic sources like service discovery. Once relabeling is done,
  they are removed from the label set. To persist internal labels so they're
  sent to Grafana Loki, rename them so they don't start with `__`. See
  [Relabeling](#relabeling) for more information.

- Labels starting with `__meta_kubernetes_pod_label_*` are "meta labels" which
  are generated based on your Kubernetes pod's labels.

  For example, if your Kubernetes pod has a label `name` set to `foobar`, then
  the `scrape_configs` section will receive an internal label
  `__meta_kubernetes_pod_label_name` with a value set to `foobar`.

- Other labels starting with `__meta_kubernetes_*` exist based on other
  Kubernetes metadata, such as the namespace of the pod
  (`__meta_kubernetes_namespace`) or the name of the container inside the pod
  (`__meta_kubernetes_pod_container_name`). Refer to
  [the Prometheus docs](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config)
  for the full list of Kubernetes meta labels.

- The `__path__` label is a special label which Promtail uses after discovery to
  figure out where the file to read is located. Wildcards are allowed, for example `/var/log/*.log` to get all files with a `log` extension in the specified directory, and `/var/log/**/*.log` for matching files and directories recursively. For a full list of options check out the docs for the [library](https://github.com/bmatcuk/doublestar) Promtail uses.

- The `__path_exclude__` label is another special label Promtail uses after
  discovery, to exclude a subset of the files discovered using `__path__` from
  being read in the current scrape_config block. It uses the same
  [library](https://github.com/bmatcuk/doublestar) to enable usage of
  wildcards and glob patterns.

- The label `filename` is added for every file found in `__path__` to ensure the
  uniqueness of the streams. It is set to the absolute path of the file the line
  was read from.

### Example of File Discovery  
To scrape a set of log files defined manually on a machine, one can use `static_configs` or `file_sd_configs`. Using static_configs, one must reload Promtail to apply modifications. Using `file_sd_configs` that reload is not needed. Promtail reloads discovery files when they are updated.  

The below excerpt of Promtail's configuration show a file_sd_configs that is used to scrape `apt` and `dpkg`'s logs.  

```yaml
scrape_configs:
 - job_name: apt-dpkg
    file_sd_configs:
      - files:
        - /etc/promtail/dpkg-apt.yaml
    refresh_interval: 5m
```  
The targets to be scraped by Promtail are defined in `/etc/promtail/dpkg-apt.yaml`. In fact, Promtail read the target to scrape in the list of file provided under `files`.  

Below is the content of `/etc/promtail/dpkg-apt.yaml`.  
```yaml
- targets: ["localhost"]
  labels:
    job: dpkg
    __path__: /var/log/dpkg.log
- targets: ["localhost"]
  labels:
    job: apt
    __path__: /var/log/apt/*.log
```  

As one can realize, `/etc/promtail/dpkg-apt.yaml` contains the list of targets we would have defined under [static_configs](https://grafana.com/docs/loki/latest/send-data/promtail/configuration/#static_configs).  
It defines two targets. The first one with label job set to `dpkg` and `__path__` specifying dpkg's log file: `/var/log/dpkg.log`. The second has two labels: the label `job` and again `__path__` specifying the path to APT's log files. This `__path__` contains a glob. Every log file matching that regular expression will be scrapped under that target.  
To summarize, the above `/etc/promtail/dpkg-apt.yaml` showcase YAML format of file_sd_config discovery file. The JSON format can be seen [here](https://grafana.com/docs/loki/latest/send-data/promtail/configuration/#file_sd_config),  

### Kubernetes Discovery

While Promtail can use the Kubernetes API to discover pods as
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

See [Relabeling](#relabeling) for more information. For more information on how to configure the service discovery see the [Kubernetes Service Discovery configuration]({{< relref "./configuration#kubernetes_sd_config" >}}).

## GCP Log scraping

Promtail supports scraping cloud resource logs such as GCS bucket logs, load balancer logs, and Kubernetes cluster logs from GCP.
Configuration is specified in the `gcplog` section, within `scrape_config`.

There are two kind of scraping strategies: `pull` and `push`.

### Pull

```yaml
  - job_name: gcplog
    gcplog:
      subscription_type: "pull" # If the `subscription_type` field is empty, defaults to `pull`
      project_id: "my-gcp-project"
      subscription: "my-pubsub-subscription"
      use_incoming_timestamp: false # default rewrite timestamps.
      use_full_line: false # default use textPayload as log line.
      labels:
        job: "gcplog"
    relabel_configs:
      - source_labels: ['__gcp_resource_type']
        target_label: 'resource_type'
      - source_labels: ['__gcp_resource_labels_project_id']
        target_label: 'project'
```
Here `project_id` and `subscription` are the only required fields.

- `project_id` is the GCP project id.
- `subscription` is the GCP pubsub subscription where Promtail can consume log entries from.

Before using `gcplog` target, GCP should be [configured]({{< relref "./cloud/gcp" >}}) with pubsub subscription to receive logs from.

It also supports `relabeling` and `pipeline` stages just like other targets.

When Promtail receives GCP logs, various internal labels are made available for [relabeling](#relabeling):
  - `__gcp_logname`
  - `__gcp_severity`
  - `__gcp_resource_type`
  - `__gcp_resource_labels_<NAME>`
  - `__gcp_labels_<NAME>`
    In the example above, the `project_id` label from a GCP resource was transformed into a label called `project` through `relabel_configs`.

### Push

```yaml
  - job_name: gcplog
    gcplog:
      subscription_type: "push"
      use_incoming_timestamp: false
      labels:
        job: "gcplog-push"
      server:
        http_listen_address: 0.0.0.0
        http_listen_port: 8080
    relabel_configs:
      - source_labels: ['__gcp_message_id']
        target_label: 'message_id'
      - source_labels: ['__gcp_attributes_logging_googleapis_com_timestamp']
        target_label: 'incoming_ts'
```

When configuring the GCP Log push target, Promtail will start an HTTP server listening on port `8080`, as configured in the `server`
section. This server exposes the single endpoint `POST /gcp/api/v1/push`, responsible for receiving logs from GCP.

For Google's PubSub to be able to send logs, **Promtail server must be publicly accessible, and support HTTPS**. For that, Promtail can be deployed
as part of a larger orchestration service like Kubernetes, which can handle HTTPS traffic through an ingress, or it can be hosted behind
a proxy/gateway, offloading the HTTPS to that component and routing the request to Promtail. Once that's solved, GCP can be [configured]({{< relref "./cloud/gcp" >}})
to send logs to Promtail.

It also supports `relabeling` and `pipeline` stages.

When Promtail receives GCP logs, various internal labels are made available for [relabeling](#relabeling):
- `__gcp_message_id`
- `__gcp_subscription_name`
- `__gcp_attributes_<NAME>`
- `__gcp_logname`
- `__gcp_severity`
- `__gcp_resource_type`
- `__gcp_resource_labels_<NAME>`
- `__gcp_labels_<NAME>`

In the example above, the `__gcp_message_id` and the `__gcp_attributes_logging_googleapis_com_timestamp` labels are
transformed to `message_id` and `incoming_ts` through `relabel_configs`. All other internal labels, for example some other attribute,
will be dropped by the target if not transformed.

## GELF

Promtail supports listening message using the [GELF](https://docs.graylog.org/docs/gelf) UDP protocol.
The GELF targets can be configured using the `gelf` stanza:

```yaml
scrape_configs:
- job_name: gelf
  gelf:
    listen_address: "0.0.0.0:12201"
    use_incoming_timestamp: true
    labels:
      job: gelf
  relabel_configs:
      - action: replace
        source_labels:
          - __gelf_message_host
        target_label: host
      - action: replace
        source_labels:
          - __gelf_message_level
        target_label: level
      - action: replace
        source_labels:
          - __gelf_message_facility
        target_label: facility
```

## Heroku Drain
Promtail supports receiving logs from a Heroku application by using a [Heroku HTTPS Drain](https://devcenter.heroku.com/articles/log-drains#https-drains).
Configuration is specified in a`heroku_drain` block within the Promtail `scrape_config` configuration.

```yaml
- job_name: heroku_drain
    heroku_drain:
      server:
        http_listen_address: 0.0.0.0
        http_listen_port: 8080
      labels:
        job: heroku_drain_docs
      use_incoming_timestamp: true
    relabel_configs:
      - source_labels: ['__heroku_drain_host']
        target_label: 'host'
      - source_labels: ['__heroku_drain_app']
        target_label: 'source'
      - source_labels: ['__heroku_drain_proc']
        target_label: 'proc'
      - source_labels: ['__heroku_drain_log_id']
        target_label: 'log_id'
```
Within the `scrape_configs` configuration for a Heroku Drain target, the `job_name` must be a Prometheus-compatible [metric name](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels).

The [server]({{< relref "./configuration#server" >}}) section configures the HTTP server created for receiving logs.
`labels` defines a static set of label values added to each received log entry. `use_incoming_timestamp` can be used to pass
the timestamp received from Heroku.

Before using a `heroku_drain` target, Heroku should be configured with the URL where the Promtail instance will be listening.
Follow the steps in [Heroku HTTPS Drain docs](https://devcenter.heroku.com/articles/log-drains#https-drains) for using the Heroku CLI
with a command like the following:

```
heroku drains:add [http|https]://HOSTNAME:8080/heroku/api/v1/drain -a HEROKU_APP_NAME
```

### Getting the Heroku application name

Note that the `__heroku_drain_app` label will contain the source of the log line, either `app` or `heroku` and not the name of the heroku application.

The easiest way to provide the actual application name is to include a query parameter when creating the heroku drain and then relabel that parameter in your scraping config, for example:

```
heroku drains:add [http|https]://HOSTNAME:8080/heroku/api/v1/drain?app_name=HEROKU_APP_NAME -a HEROKU_APP_NAME

```

And then in a relabel_config:

```yaml
    relabel_configs:
      - source_labels: ['__heroku_drain_param_app_name']
        target_label: 'app'
```

It also supports `relabeling` and `pipeline` stages just like other targets.

When Promtail receives Heroku Drain logs, various internal labels are made available for [relabeling](#relabeling):
- `__heroku_drain_host`
- `__heroku_drain_app`
- `__heroku_drain_proc`
- `__heroku_drain_log_id`
In the example above, the `project_id` label from a GCP resource was transformed into a label called `project` through `relabel_configs`.

## HTTP client

Promtail uses the Prometheus HTTP client implementation for all calls to Loki.
Therefore it can be configured using the `clients` stanza, where one or more
connections to Loki can be established:

```yaml
clients:
  - [ <client_option> ]
```

Refer to [`client_config`]({{< relref "./configuration#clients" >}}) from the Promtail
Configuration reference for all available options.

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
      matches: _TRANSPORT=kernel
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
reads. The `matches` field adds journal filters. If multiple filters are specified
matching different fields, the log entries are filtered by both, if two filters
apply to the same field, then they are automatically matched as alternatives.

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

## Kafka

Promtail supports reading message from Kafka using a consumer group.
The Kafka targets can be configured using the `kafka` stanza:

```yaml
scrape_configs:
- job_name: kafka
  kafka:
    brokers:
    - my-kafka-0.org:50705
    - my-kafka-1.org:50705
    topics:
    - ^promtail.*
    - some_fixed_topic
    labels:
      job: kafka
  relabel_configs:
      - action: replace
        source_labels:
          - __meta_kafka_topic
        target_label: topic
      - action: replace
        source_labels:
          - __meta_kafka_partition
        target_label: partition
      - action: replace
        source_labels:
          - __meta_kafka_group_id
        target_label: group
      - action: replace
        source_labels:
          - __meta_kafka_message_key
        target_label: message_key
```

Only the `brokers` and `topics` are required.
Read the [configuration]({{< relref "./configuration#kafka" >}}) section for more information.

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

- Drop the target if a label (`__service__` in the example) is empty:
```yaml
  - action: drop
    regex: ''
    source_labels:
    - __service__
```
- Drop the target if any of the `source_labels` contain a value:
```yaml
  - action: drop
    regex: .+
    separator: ''
    source_labels:
    - __meta_kubernetes_pod_label_name
    - __meta_kubernetes_pod_label_app
```
- Persist an internal label by renaming it so it will be sent to Loki:
```yaml
  - action: replace
    source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
```
- Persist all Kubernetes pod labels by mapping them, like by mapping
    `__meta_kube__meta_kubernetes_pod_label_foo` to `foo`.
```yaml
  - action: labelmap
    regex: __meta_kubernetes_pod_label_(.+)
```

Additional reading:

 - [Julien Pivotto's slides from PromConf Munich, 2017](https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749)

## Syslog Receiver

Promtail supports receiving [IETF Syslog (RFC5424)](https://tools.ietf.org/html/rfc5424)
messages from a TCP or UDP stream. Receiving syslog messages is defined in a `syslog`
stanza:

```yaml
scrape_configs:
  - job_name: syslog
    syslog:
      listen_address: 0.0.0.0:1514
      listen_protocol: tcp
      idle_timeout: 60s
      label_structured_data: yes
      labels:
        job: "syslog"
    relabel_configs:
      - source_labels: ['__syslog_message_hostname']
        target_label: 'host'
```

The only required field in the syslog section is the `listen_address` field,
where a valid network address must be provided. The default protocol for
receiving messages is TCP. To change the protocol, the `listen_protocol` field
can be changed to `udp`. Note, that UDP does not support TLS.
The `idle_timeout` can help with cleaning up stale syslog connections.
If `label_structured_data` is set,
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

For sending messages via TCP:

```
*.* action(type="omfwd" protocol="tcp" target="<promtail_host>" port="<promtail_port>" Template="RSYSLOG_SyslogProtocol23Format" TCP_Framing="octet-counted" KeepAlive="on")
```

For sending messages via UDP:

```
*.* action(type="omfwd" protocol="udp" target="<promtail_host>" port="<promtail_port>" Template="RSYSLOG_SyslogProtocol23Format")
```

## Windows Event Log

On Windows Promtail supports reading from the event log.
Windows event targets can be configured using the `windows_events` stanza:


```yaml
scrape_configs:
- job_name: windows
  windows_events:
    use_incoming_timestamp: false
    bookmark_path: "./bookmark.xml"
    eventlog_name: "Application"
    xpath_query: '*'
    labels:
      job: windows
  relabel_configs:
    - source_labels: ['computer']
      target_label: 'host'
```

When Promtail receives an event it will attach the `channel` and `computer` labels
and serialize the event in json.
You can relabel default labels via [Relabeling](#relabeling) if required.

Providing a path to a bookmark is mandatory, it will be used to persist the last event processed and allow
resuming the target without skipping logs.

Read the [configuration]({{< relref "./configuration#windows_events" >}}) section for more information.

See the [eventlogmessage]({{< relref "./stages/eventlogmessage" >}}) stage for extracting
data from the `message`.
