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
  figure out where the file to read is located. Wildcards are allowed.

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

Refer to [`client_config`](./configuration.md#client_config) from the Promtail
Configuration reference for all available options.
