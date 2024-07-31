---
menuTitle: Labels
title: Understand labels
description: Explains how to Loki uses labels to define log streams.
weight: 600
aliases:
    - ../getting-started/labels/
    - ../fundamentals/labels/
---
# Understand labels

Labels are key value pairs and can be defined as anything! We like to refer to them as metadata to describe a log stream. If you are familiar with Prometheus, there are a few labels you are used to seeing like `job` and `instance`, and I will use those in the coming examples.

The scrape configs we provide with Grafana Loki define these labels, too. If you are using Prometheus, having consistent labels between Loki and Prometheus is one of Loki's superpowers, making it incredibly [easy to correlate your application metrics with your log data](/blog/2019/05/06/how-loki-correlates-metrics-and-logs--and-saves-you-money/).

## How Loki uses labels

Labels in Loki perform a very important task: They define a stream. More specifically, the combination of every label key and value defines the stream. If just one label value changes, this creates a new stream.

If you are familiar with Prometheus, the term used there is series; however, Prometheus has an additional dimension: metric name. Loki simplifies this in that there are no metric names, just labels, and we decided to use streams instead of series.

{{% admonition type="note" %}}
Structured metadata do not define a stream, but are metadata attached to a log line.
See [structured metadata]({{< relref "./structured-metadata" >}}) for more information.
{{% /admonition %}}

## Format

Loki places the same restrictions on label naming as [Prometheus](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels):

- It may contain ASCII letters and digits, as well as underscores and colons. It must match the regex `[a-zA-Z_:][a-zA-Z0-9_:]*`.
- The colons are reserved for user defined recording rules. They should not be used by exporters or direct instrumentation.
- Unsupported characters in the label should be converted to an underscore. For example, the label `app.kubernetes.io/name` should be written as `app_kubernetes_io_name`.


## Loki labels demo

This series of examples will illustrate basic use cases and concepts for labeling in Loki.

Let's take an example Promtail/Alloy config file:

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

```
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

```
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

```
{env="dev"} <- will return all logs with env=dev, in this case this includes both log streams
```

Hopefully now you are starting to see the power of labels. By using a single label, you can query many streams. By combining several different labels, you can create very flexible log queries.

Labels are the index to Loki log data. They are used to find the compressed log content, which is stored separately as chunks. Every unique combination of label and values defines a stream, and logs for a stream are batched up, compressed, and stored as chunks.

For Loki to be efficient and cost-effective, we have to use labels responsibly. The next section will explore this in more detail.

## Cardinality

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

This regex matches every component of the log line and extracts the value of each component into a capture group. Inside the pipeline code, this data is placed in a temporary data structure that allows using it for several purposes during the processing of that log line (at which point that temp data is discarded). Much more detail about this can be found in the [Promtail pipelines]({{< relref "../../send-data/promtail/pipelines" >}}) documentation.

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

```
{job="apache",env="dev",action="GET",status_code="200"} 11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="POST",status_code="200"} 11.11.11.12 - frank [25/Jan/2000:14:00:02 -0500] "POST /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="GET",status_code="400"} 11.11.11.13 - frank [25/Jan/2000:14:00:03 -0500] "GET /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
{job="apache",env="dev",action="POST",status_code="400"} 11.11.11.14 - frank [25/Jan/2000:14:00:04 -0500] "POST /1986.js HTTP/1.1" 400 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"
```

Those four log lines would become four separate streams and start filling four separate chunks.

Any additional log lines that match those combinations of label/values would be added to the existing stream. If another unique combination of labels comes in (for example, `status_code="500"`) another new stream is created.

Imagine now if you set a label for `ip`. Not only does every request from a user become a unique stream. Every request with a different action or status_code from the same user will get its own stream.

Doing some quick math, if there are maybe four common actions (GET, PUT, POST, DELETE) and maybe four common status codes (although there could be more than four!), this would be 16 streams and 16 separate chunks. Now multiply this by every user if we use a label for `ip`.  You can quickly have thousands or tens of thousands of streams.

This is high cardinality, and it can lead to significant performance degredation.

When we talk about _cardinality_ we are referring to the combination of labels and values and the number of streams they create. High cardinality is using labels with a large range of possible values, such as `ip`, **or** combining many labels, even if they have a small and finite set of values, such as using `status_code` and `action`.

High cardinality causes Loki to build a huge index and to flush thousands of tiny chunks to the object store. Loki currently performs very poorly in this configuration. If not accounted for, high cardinality will significantly reduce the operability and cost-effectiveness of Loki.

## Optimal Loki performance with parallelization

Now you may be asking: If using too many labels—or using labels with too many values—is bad, then how am I supposed to query my logs? If none of the data is indexed, won't queries be really slow?

As we see people using Loki who are accustomed to other index-heavy solutions, it seems like they feel obligated to define a lot of labels in order to query their logs effectively. After all, many other logging solutions are all about the index, and this is the common way of thinking.

When using Loki, you may need to forget what you know and look to see how the problem can be solved differently with parallelization. Loki's superpower is breaking up queries into small pieces and dispatching them in parallel so that you can query huge amounts of log data in small amounts of time.

This kind of brute force approach might not sound ideal, but let me explain why it is.

Large indexes are complicated and expensive. Often a full-text index of your log data is the same size or bigger than the log data itself. To query your log data, you need this index loaded, and for performance, it should probably be in memory. This is difficult to scale, and as you ingest more logs, your index gets larger quickly.

Now let's talk about Loki, where the index is typically an order of magnitude smaller than your ingested log volume. So if you are doing a good job of keeping your streams and stream churn to a minimum, the index grows very slowly compared to the ingested logs.

Loki will effectively keep your static costs as low as possible (index size and memory requirements as well as static log storage) and make the query performance something you can control at runtime with horizontal scaling.

To see how this works, let's look back at our example of querying your access log data for a specific IP address. We don't want to use a label to store the IP address. Instead we use a [filter expression]({{< relref "../../query/log_queries#line-filter-expression" >}}) to query for it:

```
{job="apache"} |= "11.11.11.11"
```

Behind the scenes, Loki will break up that query into smaller pieces (shards), and open up each chunk for the streams matched by the labels and start looking for this IP address.

The size of those shards and the amount of parallelization is configurable and based on the resources you provision. If you want to, you can configure the shard interval down to 5m, deploy 20 queriers, and process gigabytes of logs in seconds. Or you can go crazy and provision 200 queriers and process terabytes of logs!

This trade-off of smaller index and parallel brute force querying vs. a larger/faster full-text index is what allows Loki to save on costs versus other systems. The cost and complexity of operating a large index is high and is typically fixed -- you pay for it 24 hours a day if you are querying it or not.

The benefits of this design mean you can make the decision about how much query power you want to have, and you can change that on demand. Query performance becomes a function of how much money you want to spend on it. Meanwhile, the data is heavily compressed and stored in low-cost object stores like S3 and GCS. This drives the fixed operating costs to a minimum while still allowing for incredibly fast query capability.
