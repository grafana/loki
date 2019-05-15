# Promtail

* [Deployment Methods](./promtail-setup.md)
* [Config and Usage Examples](./promtail-examples.md)
* [Troubleshooting](./troubleshooting.md)


## Design Documentation
   
   * [Extracting labels from logs](./design/labels.md)

## Promtail and scrape_configs

Promtail is an agent which reads log files and sends streams of log data to
the centralised Loki instances along with a set of labels. For example if you are running Promtail in Kubernetes
then each container in a single pod will usually yield a single log stream with a set of labels
based on that particular pod Kubernetes labels. You can also run Promtail outside Kubernetes, but you would
then need to customise the scrape_configs for your particular use case.

The way how Promtail finds out the log locations and extracts the set of labels is by using the *scrape_configs*
section in the Promtail yaml configuration. The syntax is the same what Prometheus uses.

The scrape_configs contains one or more *entries* which are all executed for each container in each new pod running
in the instance. If more than one entry matches your logs you will get duplicates as the logs are sent in more than
one stream, likely with a slightly different labels. Everything is based on different labels.
The term "label" here is used in more than one different way and they can be easily confused.

* Labels starting with __ (two underscores) are internal labels. They are not stored to the loki index and are
  invisible after Promtail. They "magically" appear from different sources.
* Labels starting with \_\_meta_kubernetes_pod_label_* are "meta labels" which are generated based on your kubernetes
  pod labels. Example: If your kubernetes pod has a label "name" set to "foobar" then the scrape_configs section
  will have a label \_\_meta_kubernetes_pod_label_name with value set to "foobar".
* There are other \_\_meta_kubernetes_* labels based on the Kubernetes metadadata, such as the namespace the pod is
  running (\_\_meta_kubernetes_namespace) or the name of the container inside the pod (\_\_meta_kubernetes_pod_container_name)
* The label \_\_path\_\_ is a special label which Promtail will read to find out where the log files are to be read in.
* The label `filename` is added for every file found in \_\_path\_\_ to ensure uniqueness of the streams. It contains the absolute path of the file being tailed.

The most important part of each entry is the *relabel_configs* which are a list of operations which creates,
renames, modifies or alters labels. A single scrape_config can also reject logs by doing an "action: drop" if
a label value matches a specified regex, which means that this particular scrape_config will not forward logs
from a particular log source, but another scrape_config might.

Many of the scrape_configs read labels from \_\_meta_kubernetes_* meta-labels, assign them to intermediate labels
such as \_\_service\_\_ based on a few different logic, possibly drop the processing if the \_\_service\_\_ was empty
and finally set visible labels (such as "job") based on the \_\_service\_\_ label.

In general, all of the default Promtail scrape_configs do the following:
 * They read pod logs from under /var/log/pods/$1/*.log.
 * They set "namespace" label directly from the \_\_meta_kubernetes_namespace.
 * They expect to see your pod name in the "name" label
 * They set a "job" label which is roughly "your namespace/your job name"

### Idioms and examples on different relabel_configs:

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
* Rename a metadata label into anothe so that it will be visible in the final log stream:
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
 * https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749

## Entry parser

### Overview

Each job can be configured with a `pipeline_stages` to parse and mutate your log entry.
This allows you to add more labels, correct the timestamp or entirely rewrite the log line sent to Loki.

> Rewriting labels by parsing the log entry should be done with caution, this could increase the cardinality
> of streams created by Promtail.

Aside from mutating the log entry, pipeline stages can also generate metrics which could be useful in situation where you can't instrument an application.

### Configuration

You can define a set of stages for each job by providing a map where the key is the parser name and the value the configuration.

Each stage will be executed for a given log entry (labels, timestamp and output) with the provided parser.
Stages can be skipped or included by using a stream selector (`match`). A stage can extract and replace labels, timestamp or the log output but also extract metrics. Once a stage has finished it will pass down the modified log entry to the next stage.

We currently support two parser `regex` and `json`, you can also use the key `cri` and `docker` with no configuration value, these are shortcuts for pre-defined stage to parse respectively the standard CRI and Docker log format.

Each stage configuration object can define:

* `labels`, `output` and `timestamp` to mutate the log entry.
* `metrics` to record metrics.
* `match` to skip or include different stream of logs.

The regex stage also requires an `expression` to be defined.

#### Regular expression

The regular expression stage works by evaluating matches from the provided `expression` ([RE2](https://github.com/google/re2/wiki/Syntax)) on the log line output. Named group can be use to replace values using the `source` property.

```yaml
# level=info ts=2019-05-14T20:37:45.883527108Z component=tailer msg="stopped tailing file" path=/var/log/pods/db86ab08-6829-11e9-8d16-42010a800099/grafana/5068.log
  - regex:
      expression: .*level=(?P<lvl>[a-zA-Z]+).*ts=(?P<ts>[T\d-:.Z]*).*component=(?P<component>[a-zA-Z]+)
      labels:
        level:
          source: lvl
        component:
      timestamp:
        format: RFC3339Nano
        source: ts
      match: { name="promtail" }
```

This stage adds level and component labels and will rewrite the timestamp ([see](#timestamp) for format)
As you can see if the source is the same name as the targeted property, you can simply omit it.
If the a selected named group match is missing (or can't be converted) the value won't be replaced and the execution will continue.


#### Json

The json stage load the log entry using a json parser and can then be queried for values to extract using [JMESPath](http://jmespath.org/) queries. JMESPath allows you to create interesting extraction by using pipes, filters, projections and functions.

```yaml
# {"level":"info","ts":1557865879.687435,"component":["client","peer"],"msg":"Connected to peer","host:port":"[::]:14267"}
  - json:
      labels:
        level:
        component:
          source: join(`,`,component)
      match: { app="json-log-app" }
```

Like the regex parser the source property can be omitted if the target property name (here `level`) is the same name of a root property. Value won't be replaced if the query doesn't return a valid value for the given property.

> NOTE: the output property can replaced by a sub json document.

#### Timestamp

Timestamp extraction requires to define a format property which is the [Go's `time.Parse`](https://golang.org/pkg/time/#Parse) format. We also support the same predefined layouts as mentioned by the [godoc](https://golang.org/pkg/time/#pkg-constants) (RFC3339Nano, RFC3339, ANSIC, RFC850, etc..).

```yaml
  - regex:
      expression: ts=(?P<timestamp>[T\d-:.Z]*)
      timestamp:
        format: RFC3339Nano
        source: timestamp
      match: { name="promtail" }
```

In the example above we could have omitted the source property or replaced the `RFC3339Nano` by its linux time representation `2006-01-02T15:04:05.999999999Z07:00`.

#### Metrics

Each stage can also extract a set of Prometheus metrics. Metrics are exposed on the path `/metrics` in promtail. By default a counter of log entries (`log_entries_total`) and a log size histogram (`log_entries_bytes_bucket`) per stream is computed. This means you don't need to create metrics to count status code or log level, simply mutate the log entry and add them to the labels. All custom metrics are prefixed with `promtail_custom_`.

To define metrics you need to add a `metrics` map in your stage where the key should be the unique name for the metric and the value should be its configuration. The configuration contains the `type` of the metrics, the `description`, and the `source` to record the value from. (Optionally for histograms a `buckets` property should be provided).

There are three [Prometheus metric types](https://prometheus.io/docs/concepts/metric_types/) available.

`Counter` and `Gauge` record metrics for each line parsed by adding the value. While `Histograms` observe sampled values by `buckets`.

Stream labels are automatically added to each metrics as they appear after `labels` evaluation.

```yaml
pipeline_stages:
  - regex:
      expression: "\"(?P<request_method>.*?) (?P<path>.*?)(?P<request_version> HTTP/.*)?\" (?P<status>.*?) (?P<length>.*?) (?P<time_taken>.*?)"
      match: {component=~"http.*"}
      labels:
        status:
      metrics:
      - http_request_latency:
        type: Histogram
        source: time_taken
        buckets: [0.005,0.001,0.01,0.1]
      - http_request_bytes_total:
        type: Counter
        source: length

```

The example above records the http latency and total bytes received per status code.

__While for a JSON stages you cannot omit the source property (the JMESPath expression), for regex it is totally acceptable to omit the source property in which case it will record the amount of named group matched.__

#### Filtering

You can use the `match` property with a [log stream selector](usage.md#log-stream-selector) to filter on which stream your stage will be executed.

This allows you to run stages for only specific clusters, nodes, applications, components or even levels.

```yaml
match: {cluster="us-east1", level=~"WARN|ERROR|FATAL"}
```

### Full Example

The example below gives a good glimpse of what you can achieve with a pipeline :

```yaml
scrape_configs:
- job_name: kubernetes-pods-name
  kubernetes_sd_configs: ....
  pipeline_stages:
  - regex:
      expression: .*level=(?P<level>[a-zA-Z]+).*ts=(?P<timestamp>[T\d-:.Z]*).*component=(?P<component>[a-zA-Z]+)
      labels:
        level:
        component:
      timestamp:
        format: RFC3339Nano
      match: {name="promtail"}
  - regex:
      expression: \w{1,3}.\w{1,3}.\w{1,3}.\w{1,3}(?P<output>.*)
      output:
      match: {name="nginx"}
  - json:
      labels:
        level:
      match: {name="jaeger-agent"}
- job_name: kubernetes-pods-app
  kubernetes_sd_configs: ....
  pipeline_stages:
  - regex:
      expression: .*(lvl|level)=(?P<level>[a-zA-Z]+).*(logger|component)=(?P<component>[a-zA-Z]+)
      labels:
        level:
        component:
      match: {app=~"grafana|prometheus"}
  - regex:
      expression: .*(?P<panic>panic: .*)
      match: {app="some-app"}
      metrics:
      - panic_total:
          type: Counter
          description: "total count of panic"

```

This configuration will set the log level and component label  using json or regular expression.
We've also stripped out the IP field of an Nginx access log and created a golang panic counter for my application `some-app`.