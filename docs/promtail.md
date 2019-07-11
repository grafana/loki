# Promtail

  * [Scrape Configs](#scrape-configs)
  * [Entry Parsing](#entry-parser)
  * [Deployment Methods](./promtail-setup.md)
  * [Promtail API](./promtail-api.md)
  * [Config and Usage Examples](./promtail-examples.md)
  * [Troubleshooting](./troubleshooting.md)

## Scrape Configs

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
 * https://www.slideshare.net/roidelapluie/taking-advantage-of-prometheus-relabeling-109483749

## Entry parser

### Overview

Each job can be configured with a `pipeline_stages` to parse and mutate your log entry.
This allows you to add more labels, correct the timestamp or entirely rewrite the log line sent to Loki.

> Rewriting labels by parsing the log entry should be done with caution, this could increase the cardinality
> of streams created by Promtail.

Aside from mutating the log entry, pipeline stages can also generate metrics which could be useful in situation where you can't instrument an application.

See [Processing Log Lines](./logentry/processing-log-lines.md) for a detailed pipeline description

#### Labels

[The original design doc](./design/labels.md) for labels.  Post implementation we have strayed quit a bit from the config examples, though the pipeline idea was maintained.

See the [pipeline label docs](./logentry/processing-log-lines.md#labels) for more info on creating labels from log content.

#### Metrics

Metrics can also be extracted from log line content as a set of Prometheus metrics. Metrics are exposed on the path `/metrics` in promtail. By default a log size histogram (`log_entries_bytes_bucket`) per stream is computed. This means you don't need to create metrics to count status code or log level, simply parse the log entry and add them to the labels. All custom metrics are prefixed with `promtail_custom_`.

There are three [Prometheus metric types](https://prometheus.io/docs/concepts/metric_types/) available.

`Counter` and `Gauge` record metrics for each line parsed by adding the value. While `Histograms` observe sampled values by `buckets`.

See the [pipeline metric docs](./logentry/processing-log-lines.md#metrics) for more info on creating metrics from log content.