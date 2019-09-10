# Configuration

## `scrape_configs` (Target Discovery)
The way how Promtail finds out the log locations and extracts the set of labels
is by using the `scrape_configs` section in the `promtail.yaml` configuration
file. The syntax is equal to what [Prometheus
uses](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config).

The `scrape_configs` contains one or more *entries* which are all executed for
each discovered target (read each container in each new pod running in the instance):
```yaml
scrape_configs:
  - job_name: local
    static_configs:
      - ...
    
  - job_name: kubernetes
    kubernetes_sd_config:
      - ...
```

If more than one entry matches your logs, you will get duplicates as the logs are
sent in more than one stream, likely with a slightly different labels.

There are different types of labels present in Promtail:

* Labels starting with `__` (two underscores) are internal labels. They usually
  come from dynamic sources like the service discovery. Once relabeling is done,
  they are removed from the label set. To persist those, rename them to
  something not starting with `__`.
* Labels starting with `__meta_kubernetes_pod_label_*` are "meta labels" which
  are generated based on your kubernetes pod labels.  
  Example: If your kubernetes pod has a label `name` set to `foobar` then the
  `scrape_configs` section will have a label `__meta_kubernetes_pod_label_name`
  with value set to `foobar`.
* There are other `__meta_kubernetes_*` labels based on the Kubernetes
  metadadata, such as the namespace the pod is running in
  (`__meta_kubernetes_namespace`) or the name of the container inside the pod
  (`__meta_kubernetes_pod_container_name`). Refer to [the Prometheus
  docs](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config)
  for the full list.
* The label `__path__` is a special label which Promtail will use afterwards to
  figure out where the file to be read is located. Wildcards are allowed.
* The label `filename` is added for every file found in `__path__` to ensure
  uniqueness of the streams. It contains the absolute path of the file the line
  was read from.

## `relabel_configs` (Relabeling)
The most important part of each entry is the `relabel_configs` stanza, which is a list
of operations to create, rename, modify or alter the labels. 

A single `scrape_config` can also reject logs by doing an `action: drop` if a label value
matches a specified regex, which means that this particular `scrape_config` will
not forward logs from a particular log source.  
This does not mean that other `scrape_config`'s might not do, though.

Many of the `scrape_configs` read labels from `__meta_kubernetes_*` meta-labels,
assign them to intermediate labels such as `__service__` based on 
different logic, possibly drop the processing if the `__service__` was empty
and finally set visible labels (such as `job`) based on the `__service__`
label.

In general, all of the default Promtail `scrape_configs` do the following:

 * They read pod logs from under `/var/log/pods/$1/*.log`.
 * They set `namespace` label directly from the `__meta_kubernetes_namespace`.
 * They expect to see your pod name in the `name` label
 * They set a `job` label which is roughly `namespace/job`

#### Examples

* Drop the processing if a label is empty:
```yaml
  - action: drop
    regex: ^$
    source_labels:
    - __service__
```
* Drop the processing if any of these labels contains a value:
```yaml
  - action: drop
    regex: .+
    separator: ''
    source_labels:
    - __meta_kubernetes_pod_label_name
    - __meta_kubernetes_pod_label_app
```
* Rename a metadata label into another so that it will be visible in the final log stream:
```yaml
  - action: replace
    source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
```
* Convert all of the Kubernetes pod labels into visible labels:
```yaml
  - action: labelmap
    regex: __meta_kubernetes_pod_label_(.+)
```


Additional reading:

 * [Julien Pivotto's slides from PromConf Munich, 2017](https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749)
 
## `client_option` (HTTP Client)
Promtail uses the Prometheus HTTP client implementation for all calls to Loki.  
Therefore, you can configure it using the `client` stanza:
```yaml
client: [ <client_option> ]
```

Reference for `client_option`:
```yaml
# Sets the `url` of loki api push endpoint
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

#### Ship to multiple Loki Servers
Promtail is able to push logs to as many different Loki servers as you like. Use
`clients` instead of `client` if needed:
```yaml
# Single Loki
client: [ <client_option> ]

# Multiple Loki instances
clients: 
  - [ <client_option> ]
```
