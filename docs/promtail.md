## Promtail and scrape_configs

Promtail is an agent which reads the Kubernetes pod log files and sends streams of log data to
the centralised Loki instances along with a set of labels. Each container in a single pod will usually yield a
single log stream with a set of labels based on that particular pod Kubernetes labels.

The way how Promtail finds out the log locations and extracts the set of labels is by using the *scrape_configs*
section in the Promtail yaml configuration. The syntax is the same what Prometheus uses.

The scrape_configs contains one or more *entries* which are all executed for each container in each new pod running
in the instance. If more than one entry matches your logs you will get duplicates as the logs are sent in more than
one stream, likely with a slightly different labels. Everything is based on different labels.
The term "label" here is used in more than one different way and they can be easily confused.

* Labels starting with __ (two underscores) are internal labels. They are not stored to the loki index and are
  invisible after Promtail. They "magically" appear from different sources.
* Labels starting with __meta_kubernetes_pod_label_* are "meta labels" which are generated based on your kubernetes
  pod labels. Example: If your kubernetes pod has a label "name" set to "foobar" then the scrape_configs section
  will have a label __meta_kubernetes_pod_label_name with value set to "foobar".
* There are other __meta_kubernetes_* labels based on the Kubernetes metadadata, such as the namespace the pod is
  running (__meta_kubernetes_namespace) or the name of the container inside the pod (__meta_kubernetes_pod_container_name)
* The label __path__ is a special label which Promtail will read to find out where the log files are to be read in.

The most important part of each entry is the *relabel_configs* which are a list of operations which creates,
renames, modifies or alters labels. A single scrape_config can also reject logs by doing an "action: drop" which means
that this particular scrape_config will not forward logs from a particular pod, but another scrape_config might.

Many of the scrape_configs read labels from __meta_kubernetes_* meta-labels, assign them to intermediate labels
such as __service__ based on a few different logic, possibly drop the processing if the __service__ was empty
and finally set visible labels (such as "job") based on the __service__ label.

In general, all of the default Promtail scrape_configs do the following:
 * They read pod logs from under /var/log/pods/$1/*.log.
 * They set "namespace" label directly from the __meta_kubernetes_namespace.
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