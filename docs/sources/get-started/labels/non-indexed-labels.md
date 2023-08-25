---
menuTitle: Non-indexed labels
title: What are non-indexed labels
description: Attaching metadata to logs.
---
# What are non-indexed labels

{{% admonition type="warning" %}}
Non-indexed labels is an experimental feature and is subject to change in future releases of Grafana Loki.
{{% /admonition %}}

One of the powerful features of Loki is parsing logs at query time to extract metadata and build labels out of it.
However, the parsing of logs at query time comes with a cost which can be significantly high for, as an example,
large json blobs or a poorly written query using complex regex patterns.

In addition, the data extracted from logs at query time is usually high cardinality, which can’t be stored
in the index as it would increase the cardinality too much, and therefore reduce the performance of the index.

Non-indexed labels are a way to attach metadata to logs without indexing them. Examples of useful metadata are
trace IDs, user IDs, and any other label that is often used in queries but has high cardinality and is expensive
to extract at query time.

## Attaching non-indexed labels to log lines

You have the option to attach non-indexed labels to log lines in the push payload along with each log line and the timestamp.
For more information on how to push logs to Loki via the HTTP endpoint, refer to the [HTTP API documentation]({{< relref "../../reference/api#push-log-entries-to-loki" >}}).

Alternatively, you can use the Grafana Agent or Promtail to extract and attach non-indexed labels to your log lines.
See the [Promtail: Non-indexed labels stage]({{< relref "../../send-data/promtail/stages/non_indexed_labels" >}}) for more information.

## Querying non-indexed labels

Non-indexed labels are extracted automatically for each returned log line and added to the labels returned for the query.
You can use non-indexed labels to filter log line using a [label filter expression]({{< relref "../../query/log_queries#label-filter-expression" >}}).

For example, if you have a non-indexed label `trace_id` attached to some of your log lines, you can filter log lines using:

```logql
{job="example"} | trace_id="0242ac120002"`
```

Of course, you can filter by multiple non-indexed labels at the same time:

```logql
{job="example"} | trace_id="0242ac120002" | user_id="superUser123"
```

Note that since non-indexed labels are extracted automatically to the results labels, some metric queries might return 
an error like `maximum of series (50000) reached for a single query`. You can use the [Keep]({{< relref "../../query/log_queries#keep-labels-expression" >}}) and [Drop]({{< relref "../../query/log_queries#drop-labels-expression" >}}) stages to filter out labels that you don't need.
For example:

```logql
count_over_time({job="example"} | trace_id="0242ac120002" | keep job  [5m])
```
