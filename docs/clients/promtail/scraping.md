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
  sent to Loki, rename them so they don't start with `__`.

* Labels starting with `__meta_kubernetes_pod_label_*` are "meta labels" which
  are generated based on your Kubernetes pod's labels.

  For exmaple, if your Kubernetes pod has a label `name` set to `foobar`, then
  the `scrape_configs` section will recieve an internal label
  `__meta_kubernetes_pod_label_name` with a value set to `foobar`.

* Other labels starting with `__meta_kubernetes_*` exist based on other
  Kubernetes metadata,s such as the namespace of the pod
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

Note that while promtail can utilize the Kubernetes API to discover pods as
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

A common usecase of `relabel_configs` is to transform an internal label such
as `__meta_kubernetes_*` into an intermediate internal label such as
`__service__`. The intermediate internal label may then be dropped based on
value or transformed to a final external label, such as `__job__`.

### Examples

* Drop the target if a label is empty:
```yaml
  - action: drop
    regex: ^$
    source_labels:
    - __service__
```
* Drop the target if any of these labels contains a value:
```yaml
  - action: drop
    regex: .+
    separator: ''
    source_labels:
    - __meta_kubernetes_pod_label_name
    - __meta_kubernetes_pod_label_app
```
* Rename an internal label into an external label so that it will be sent to Loki:
```yaml
  - action: replace
    source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
```
* Convert all of the Kubernetes pod labels into external labels:
```yaml
  - action: labelmap
    regex: __meta_kubernetes_pod_label_(.+)
```

Additional reading:

 * [Julien Pivotto's slides from PromConf Munich, 2017](https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749)

## HTTP client options

Promtail uses the Prometheus HTTP client implementation for all calls to Loki.
Therefore it can be configured using the `client` stanza:

```yaml
client: [ <client_option> ]
```

Reference for `client_option`:

```yaml
# The endpoint to push to Loki, specified in the Loki configuration as
# http_listen_host and http_listen_port. If Loki is running in microservices
# mode, this is the HTTP URL for the Distributor.
url: http[s]://<host>:<port>/api/prom/push

# Sets the `Authorization` header on every promtail request with the
# configured username and password.
# password and password_file are mutually exclusive.
basic_auth:
  username: <string>
  password: <secret>
  password_file: <string>

# Sets the `Authorization` header on every promtail request with
# the configured bearer token. It is mutually exclusive with `bearer_token_file`.
bearer_token: <secret>

# Sets the `Authorization` header on every promtail request with the bearer token
# read from the configured file. It is mutually exclusive with `bearer_token`.
bearer_token_file: /path/to/bearer/token/file

# Configures the promtail request's TLS settings.
tls_config:
  # CA certificate to validate API server certificate with.
  # If not provided Trusted CA from system will be used.
  ca_file: <filename>

  # Certificate and key files for client cert authentication to the server.
  cert_file: <filename>
  key_file: <filename>

  # ServerName extension to indicate the name of the server.
  # https://tools.ietf.org/html/rfc4366#section-3.1
  server_name: <string>

  # Disable validation of the server certificate.
  insecure_skip_verify: <boolean>

# Optional proxy URL.
proxy_url: <string>

# Maximum wait period before sending batch
batchwait: 1s

# Maximum batch size to accrue before sending, unit is byte
batchsize: 102400

# Maximum time to wait for server to respond to a request
timeout: 10s

backoff_config:
  # Initial backoff time between retries
  minbackoff: 100ms
  # Maximum backoff time between retries
  maxbackoff: 5s
  # Maximum number of retires when sending batches, 0 means infinite retries
  maxretries: 5

# The labels to add to any time series or alerts when communicating with loki
external_labels: {}
```

### Ship to multiple Loki Servers

Promtail is able to push logs to as many different Loki servers as you like. Use
`clients` instead of `client` to achieve this:

```yaml
# Single Loki
client: [ <client_option> ]

# Multiple Loki instances
clients:
  - [ <client_option> ]
```

