---
menuTitle: Labels
title: Understand labels
description: Explains how Loki uses labels to define log streams.
weight: 600
aliases:
    - ../getting-started/labels/
    - ../fundamentals/labels/
---
# Understand labels

Labels are a crucial part of Loki. They allow Loki to organize and group together log messages into log streams. Each log stream must have at least one label to be stored and queried in Loki.

In this topic we'll learn about labels and why your choice of labels is important when shipping logs to Loki.

{{< admonition type="note" >}}
Labels are intended to store [low-cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/) values that describe the source of your logs. If you frequently search high-cardinality data in your logs, you should use [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/).
{{< /admonition >}}

## Understand labels

In Loki, the content of each log line is not indexed. Instead, log entries are grouped into streams which are indexed with labels.

A label is a key-value pair, for example all of the following are labels:

- deployment_environment = development
- cloud_region = us-west-1
- namespace = grafana-server

A set of log messages which shares all the labels above would be called a log stream. When Loki performs searches, it first looks for all messages in your chosen stream, and then iterates through the logs in the stream to perform your query.

Labeling will affect your queries, which in turn will affect your dashboards.
It’s worth spending the time to think about your labeling strategy before you begin ingesting logs to Loki.

## Default labels for all users

Loki does not parse or process your log messages on ingestion. However, depending on which client you use to collect logs, you may have some labels automatically applied to your logs.

`service_name`

Loki automatically tries to populate a default `service_name` label while ingesting logs. The service name label is used to find and explore logs in the following Grafana and Grafana Cloud features:

- Logs Drilldown
- Grafana Cloud Application Observability

{{< admonition type="note" >}}
If you are already applying a `service_name`, Loki will use that value.
{{< /admonition >}}

Loki will attempt to create the `service_name` label by looking for the following labels in this order:

- service_name
- service
- app
- application
- name
- app_kubernetes_io_name
- container
- container_name
- component
- workload
- job

If no label is found matching the list, a value of `unknown_service` is applied.

You can change this list by providing a list of labels to `discover_service_name` in the [limits_config](/docs/loki/<LOKI_VERSION>/configure/#limits_config) block.  If you are using Grafana Cloud, contact support to configure this setting.

## Default labels for OpenTelemetry

If you are using either Grafana Alloy or the OpenTelemetry Collector as your Loki client, then Loki automatically assigns some of the OTel resource attributes as labels. Resource attributes map well to index labels in Loki, since both usually identify the source of the logs.

By default, the following resource attributes will be stored as labels, with periods (`.`) replaced with underscores (`_`), while the remaining attributes are stored as [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) with each log entry:

- cloud.availability_zone
- cloud.region
- container.name
- deployment.environment
- k8s.cluster.name
- k8s.container.name
- k8s.cronjob.name
- k8s.daemonset.name
- k8s.deployment.name
- k8s.job.name
- k8s.namespace.name
- k8s.pod.name
- k8s.replicaset.name
- k8s.statefulset.name
- service.instance.id
- service.name
- service.namespace

{{% admonition type="note" %}}
Because Loki has a default limit of 15 index labels, we recommend storing only select resource attributes as labels. Although the default config selects more than 15 Resource Attributes, some are mutually exclusive.
{{% /admonition %}}

{{< admonition type="tip" >}}
For Grafana Cloud Logs, see the [current OpenTelemetry guidance](https://grafana.com/docs/grafana-cloud/send-data/otlp/otlp-format-considerations/#logs).
{{< /admonition >}}

The default list of resource attributes to store as labels can be configured using `default_resource_attributes_as_index_labels` under the [distributor's otlp_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#distributor). You can set global limits using [limits_config.otlp_config](/docs/loki/<LOKI_VERSION>/configure/#limits_config). If you are using Grafana Cloud, contact support to configure this setting.

## Labeling is iterative

You want to start with a small set of labels. While accepting the default labels assigned by Grafana Alloy or the OpenTelemetry Collector or the Kubernetes Monitoring Helm chart may meet your needs, over time you may find that you need to modify your labeling strategy.

Once you understand how your first set of labels works and you understand how to apply and query with those labels, you may find that they don’t meet your query patterns.  You may need to modify or change your labels and test your queries again.

Settling on the right labels for your business needs may require multiple rounds of testing. This should be expected as you continue to tune your Loki environment to meet your business requirements.

## Create low cardinality labels

[Cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/) refers to the combination of unique labels and values which impacts the number of log streams you create.  High cardinality causes Loki to build a huge index and to flush thousands of tiny chunks to the object store. Loki performs very poorly when your labels have high cardinality. If not accounted for, high cardinality will significantly reduce the performance and cost-effectiveness of Loki.

High cardinality can result from using labels with an unbounded or large set of possible values, such as timestamp or ip_address **or** applying too many labels, even if they have a small and finite set of values.

High cardinality can lead to significant performance degradation. Prefer fewer labels, which have bounded values.

## Creating custom labels

{{< admonition type="tip" >}}
Many log collectors such as Grafana Alloy, or the Kubernetes Monitoring Helm chart, will automatically assign appropriate labels for you, so you don't need to create your own labeling strategy.  For most use cases, you can just accept the default labels.
{{< /admonition >}}

Usually, labels describe the source of the log, for example:

- the namespace or additional logical grouping of applications
- cluster, and/or region of where the logs were produced
- the filename of the source log file on disk
- the hostname where the log was produced, if the environment has individually named machines or virtual machines.  If you have an environment with ephemeral machines or virtual machines, the hostname should be stored in [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/).

If your logs had the example labels above, then you might query them in LogQL like this:

`{namespace="mynamespace", cluster="cluster123" filename="/var/log/myapp.log"}`

Unlike index-based log aggregators, Loki doesn't require you to create a label for every field that you might wish to search in your log content. Labels are only needed to organize and identify your log streams. Loki performs searches by iterating over a log stream in a highly parallelized fashion to look for a given string.

For more information on how Loki performs searches, see the [Query section](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).

This means that you don't need to add labels for things inside the log message, such as:

- log level
- log message
- exception name

That being said, in some cases you may wish to add some extra labels, which can help to narrow down your log streams even further. When adding custom labels, follow these principles:

- DO use fewer labels, aim to have 10 - 15  labels at a maximum. Fewer labels means a smaller index, which leads to better performance.
- DO be as specific with your labels you can be, the less searching that Loki has to do, the faster your result is returned.
- DO create labels with long-lived values, not unbounded values. To be a good label, we want something that has a stable set of values over time -- even if there are a lot of them.  If just one label value changes, this creates a new stream.
- DO create labels based on terms that your users will actually be querying on.
- DON'T create labels for very specific searches (for example, user ID or customer ID) or seldom used searches (searches performed maybe once a year).

### Label format

Loki places the same restrictions on label naming as [Prometheus](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels):

- It may contain ASCII letters and digits, as well as underscores and colons. It must match the regex `[a-zA-Z_:][a-zA-Z0-9_:]*`.
- Unsupported characters in the label should be converted to an underscore. For example, the label `app.kubernetes.io/name` should be written as `app_kubernetes_io_name`.
- However, do not begin and end your label names with double underscores, as this naming convention is used for internal labels, for example, \__stream_shard__, that are hidden by default in the label browser, query builder, and autocomplete to avoid creating confusion for users.

In Loki, you do not need to add labels based on the content of the log message.

### Labels and ingestion order

Loki supports ingesting out-of-order log entries. Out-of-order writes are enabled globally by default, but can be disabled/enabled on a cluster or per-tenant basis.  If you plan to ingest out-of-order log entries, your label selection is important.  We recommend trying to find a way to use labels to separate the streams so they can be ingested separately.

Entries in a given log stream (identified by a given set of label names & values) must be ingested in order, within the default two hour time window. If you try to send entries that are too old for a given log stream, Loki will respond with the error too far behind.

For systems with different ingestion delays and shipping, use labels to create separate streams. Instead of:

`{environment="production"}`

You may separate the log stream into:

`{environment="production", app="slow_app"}`
`{environment="production", app="fast_app"}`

Now the "fast_app" and "slow_app" will ship logs to different streams, allowing each to maintain their order of ingestion.

## Loki labels examples

The way that labels are added to logs is configured in the client that you use to send logs to Loki.  The specific configuration will be different for each client.

### Alloy example

Grafana Labs recommends using Grafana Alloy to send logs to Loki.  Here is an example configuration:

```alloy

local.file_match "tmplogs" {
    path_targets = [{"__path__" = "/tmp/alloy-logs/*.log"}]
}

loki.source.file "local_files" {
    targets    = local.file_match.tmplogs.targets
    forward_to = [loki.process.add_new_label.receiver]
}

loki.process "add_new_label" {
    // Extract the value of "level" from the log line and add it to the extracted map as "extracted_level"
    // You could also use "level" = "", which would extract the value of "level" and add it to the extracted map as "level"
    // but to make it explicit for this example, we will use a different name.
    //
    // The extracted map will be covered in more detail in the next section.
    stage.logfmt {
        mapping = {
            "extracted_level" = "level",
        }
    }

    // Add the value of "extracted_level" from the extracted map as a "level" label
    stage.labels {
        values = {
            "level" = "extracted_level",
        }
    }

    forward_to = [loki.relabel.add_static_label.receiver]
}

loki.relabel "add_static_label" {
    forward_to = [loki.write.local_loki.receiver]

    rule {
        target_label = "os"
        replacement  = constants.os
    }
}

loki.write "local_loki" {
    endpoint {
        url = "http://localhost:3100/loki/api/v1/push"
    }
}
```

### Promtail example

Here is an example of a Promtail configuration to send logs to Loki:

```yaml
scrape_configs:
- job_name: system
  pipeline_stages:
  static_configs:
  - targets:
     - localhost
    labels:
     job: syslog
     __path__: /var/log/syslog
```

This config will tail one file and assign one label: `job=syslog`. This will create one stream in Loki.

You could query it like this:

```bash
{job="syslog"}
```

Now let’s expand the example a little:

```yaml
scrape_configs:
- job_name: system
  pipeline_stages:
  static_configs:
  - targets:
     - localhost
    labels:
     job: syslog
     __path__: /var/log/syslog
- job_name: apache
  pipeline_stages:
  static_configs:
  - targets:
     - localhost
    labels:
     job: apache
     __path__: /var/log/apache.log
```

Now we are tailing two files. Each file gets just one label with one value, so Loki will now be storing two streams.

We can query these streams in a few ways:

```nohighlight
{job="apache"} <- show me logs where the job label is apache
{job="syslog"} <- show me logs where the job label is syslog
{job=~"apache|syslog"} <- show me logs where the job is apache **OR** syslog
```

In that last example, we used a regex label matcher to view log streams that use the job label with one of two possible values. Now consider how an additional label could also be used:

```yaml
scrape_configs:
- job_name: system
  pipeline_stages:
  static_configs:
  - targets:
     - localhost
    labels:
     job: syslog
     env: dev
     __path__: /var/log/syslog
- job_name: apache
  pipeline_stages:
  static_configs:
  - targets:
     - localhost
    labels:
     job: apache
     env: dev
     __path__: /var/log/apache.log
```

Now instead of a regex, we could do this:

```nohighlight
{env="dev"} <- will return all logs with env=dev, in this case this includes both log streams
```

Hopefully, now you are starting to see the power of labels. By using a single label, you can query many streams. By combining several different labels, you can create very flexible log queries.

Labels are the index to Loki log data. They are used to find the compressed log content, which is stored separately as chunks. Every unique combination of labels and values defines a stream and logs for a stream are batched up, compressed, and stored as chunks.

For Loki to be efficient and cost-effective, we have to use labels responsibly. The next section will explore this in more detail.

### Cardinality examples

The two previous examples use statically defined labels with a single value; however, there are ways to dynamically define labels. Let's take a look using the Apache log and a massive regex you could use to parse such a log line:

```nohighlight
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

```yaml
- job_name: system
  pipeline_stages:
     - regex:
       expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status_code>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$"
   - labels:
       action:
       status_code:
  static_configs:
  - targets:
     - localhost
    labels:
     job: apache
     env: dev
     __path__: /var/log/apache.log
```

This regex matches every component of the log line and extracts the value of each component into a capture group. Inside the pipeline code, this data is placed in a temporary data structure that allows use for several purposes during the processing of that log line (at which point that temp data is discarded). Much more detail about this can be found in the [Promtail pipelines](../../send-data/promtail/pipelines/) documentation.

From that regex, we will be using two of the capture groups to dynamically set two labels based on content from the log line itself:

action (for example, `action="GET"`, `action="POST"`)

status_code (for example, `status_code="200"`, `status_code="400"`)

And now let's walk through a few example lines:

```nohighlight
11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
11.11.11.12 - frank [25/Jan/2000:14:00:02 -0500] "POST /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
11.11.11.13 - frank [25/Jan/2000:14:00:03 -0500] "GET /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
11.11.11.14 - frank [25/Jan/2000:14:00:04 -0500] "POST /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

In Loki the following streams would be created:

```nohighlight
{job="apache",env="dev",action="GET",status_code="200"} 11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="POST",status_code="200"} 11.11.11.12 - frank [25/Jan/2000:14:00:02 -0500] "POST /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="GET",status_code="400"} 11.11.11.13 - frank [25/Jan/2000:14:00:03 -0500] "GET /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="POST",status_code="400"} 11.11.11.14 - frank [25/Jan/2000:14:00:04 -0500] "POST /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

Those four log lines would become four separate streams and start filling four separate chunks.

Any additional log lines that match those combinations of labels/values would be added to the existing stream. If another unique combination of labels comes in (for example, `status_code="500"`) another new stream is created.

Imagine now if you set a label for `ip`. Not only does every request from a user become a unique stream. Every request with a different action or status_code from the same user will get its own stream.

Doing some quick math, if there are maybe four common actions (GET, PUT, POST, DELETE) and maybe four common status codes (although there could be more than four!), this would be 16 streams and 16 separate chunks. Now multiply this by every user if we use a label for `ip`.  You can quickly have thousands or tens of thousands of streams.
