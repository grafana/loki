---
menuTitle: Structured metadata
title: What is structured metadata
description: Attaching metadata to logs.
---
# What is structured metadata

{{% admonition type="warning" %}}
Structured metadata is an experimental feature and is subject to change in future releases of Grafana Loki. This feature is not yet available for Cloud Logs users.
{{% /admonition %}}

{{% admonition type="warning" %}}
Structured metadata was added to chunk format V4 which is used if the schema version is greater or equal to `13`. (See [Schema Config]({{< relref "../../storage#schema-config" >}}) for more details about schema versions. )
{{% /admonition %}}

One of the powerful features of Loki is parsing logs at query time to extract metadata and build labels out of it.
However, the parsing of logs at query time comes with a cost which can be significantly high for, as an example,
large json blobs or a poorly written query using complex regex patterns.

In addition, the data extracted from logs at query time is usually high cardinality, which can’t be stored
in the index as it would increase the cardinality too much, and therefore reduce the performance of the index.

Structured metadata is a way to attach metadata to logs without indexing them. Examples of useful metadata are
trace IDs, user IDs, and any other label that is often used in queries but has high cardinality and is expensive
to extract at query time.

## Attaching structured metadata to log lines

You have the option to attach structured metadata to log lines in the push payload along with each log line and the timestamp.
For more information on how to push logs to Loki via the HTTP endpoint, refer to the [HTTP API documentation]({{< relref "../../reference/api#ingest-logs" >}}).

Alternatively, you can use the Grafana Agent or Promtail to extract and attach structured metadata to your log lines.
See the [Promtail: Structured metadata stage]({{< relref "../../send-data/promtail/stages/structured_metadata" >}}) for more information.

## Querying structured metadata

Structured metadata is extracted automatically for each returned log line and added to the labels returned for the query.
You can use labels of structured metadata to filter log line using a [label filter expression]({{< relref "../../query/log_queries#label-filter-expression" >}}).

For example, if you have a label `trace_id` attached to some of your log lines as structured metadata, you can filter log lines using:

```logql
{job="example"} | trace_id="0242ac120002"`
```

Of course, you can filter by multiple labels of structured metadata at the same time:

```logql
{job="example"} | trace_id="0242ac120002" | user_id="superUser123"
```

Note that since structured metadata is extracted automatically to the results labels, some metric queries might return 
an error like `maximum of series (50000) reached for a single query`. You can use the [Keep]({{< relref "../../query/log_queries#keep-labels-expression" >}}) and [Drop]({{< relref "../../query/log_queries#drop-labels-expression" >}}) stages to filter out labels that you don't need.
For example:

```logql
count_over_time({job="example"} | trace_id="0242ac120002" | keep job  [5m])
```
