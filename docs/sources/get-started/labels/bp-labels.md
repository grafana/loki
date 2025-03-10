---
title: Label best practices
menuTitle:  Label best practices
description: Describes best practices for using labels in Grafana Loki.
aliases:
- ../../best-practices/ # /docs/loki/<LOKI_VERSION>/best-practices/
weight: 700
---
# Label best practices

Grafana Loki is under active development, and we are constantly working to improve performance. But here are some of the most current best practices for labels that will give you the best experience with Loki.

## Static labels are good

Use labels for things like regions, clusters, servers, applications, namespaces, and environments. They will be fixed for a given system/app and have bounded values. Use static labels to make it easier to query your logs in a logical sense (for example, show me all the logs for a given application and specific environment, or show me all the logs for all the apps on a specific host).

## Use dynamic labels sparingly

Too many label value combinations leads to too many streams. The penalties for that in Loki are a large index and small chunks in the store, which in turn can actually reduce performance.

To avoid those issues, don't add a label for something until you know you need it! Use filter expressions (`|= "text"`, `|~ "regex"`, …) and brute force those logs. It works -- and it's fast.

If you often parse a label from a log line at query time, the label has a high cardinality, and extracting that label is expensive in terms of performance; consider extracting the label on the client side
attaching it as [structured metadata](../structured-metadata/) to log lines .

From early on, we have set a label dynamically using Promtail pipelines for `level`. This seemed intuitive for us as we often wanted to only show logs for `level="error"`; however, we are re-evaluating this now as writing a query. `{app="loki"} |= "level=error"` is proving to be just as fast for many of our applications as `{app="loki",level="error"}`.

This may seem surprising, but if applications have medium to low volume, that label causes one application's logs to be split into up to five streams, which means 5x chunks being stored.  And loading chunks has an overhead associated with it. Imagine now if that query were `{app="loki",level!="debug"}`. That would have to load **way** more chunks than `{app="loki"} != "level=debug"`.

Above, we mentioned not to add labels until you _need_ them, so when would you _need_ labels?? A little farther down is a section on `chunk_target_size`. If you set this to 1MB (which is reasonable), this will try to cut chunks at 1MB compressed size, which is about 5MB-ish of uncompressed logs (might be as much as 10MB depending on compression). If your logs have sufficient volume to write 5MB in less time than `max_chunk_age`, or **many** chunks in that timeframe, you might want to consider splitting it into separate streams with a dynamic label.

What you want to avoid is splitting a log file into streams, which result in chunks getting flushed because the stream is idle or hits the max age before being full. As of [Loki 1.4.0](/blog/2020/04/01/loki-v1.4.0-released-with-query-statistics-and-up-to-300x-regex-optimization/), there is a metric which can help you understand why chunks are flushed `sum by (reason) (rate(loki_ingester_chunks_flushed_total{cluster="dev"}[1m]))`.

It’s not critical that every chunk be full when flushed, but it will improve many aspects of operation. As such, our current guidance here is to avoid dynamic labels as much as possible and instead favor filter expressions. For example, don’t add a `level` dynamic label, just `|= "level=debug"` instead.

Here are some best practices for using dynamic labels with Loki:
- Ensure the labels have low cardinality, ideally limited to tens of values.
- Use labels with long-lived values, such as the initial segment of an HTTP path: `/load`, `/save`, `/update`.
  - Do not extract ephemeral values like a trace ID or an order ID into a label; the values should be static, not dynamic.
- Only add labels that users will frequently use in their queries.
  - Don’t increase the size of the index and fragment your log streams if nobody is actually using these labels. This will degrade performance. 

## Label values must always be bounded

If you are dynamically setting labels, never use a label which can have unbounded or infinite values. This will always result in big problems for Loki.

Try to keep values bounded to as small a set as possible. We don't have perfect guidance as to what Loki can handle, but think single digits, or maybe 10’s of values for a dynamic label. This is less critical for static labels. For example, if you have 1,000 hosts in your environment it's going to be just fine to have a host label with 1,000 values.

As a general rule, you should try to keep any single tenant in Loki to less than **100,000 active streams**, and less than a million streams in a 24-hour period.  These values are for HUGE tenants, sending more than **10 TB** a day. If your tenant is 10x smaller, you should have at least 10x fewer labels.

## Be aware of dynamic labels applied by clients

Loki has several client options: [Grafana Alloy](https://grafana.com/docs/alloy/latest/), [Promtail](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/) (which also supports systemd journal ingestion and TCP-based syslog ingestion), [Fluentd](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentd/), [Fluent Bit](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/), a [Docker plugin](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/docker-driver/), and more.

Each of these come with ways to configure what labels are applied to create log streams. But be aware of what dynamic labels might be applied.
Use the Loki series API to get an idea of what your log streams look like and see if there might be ways to reduce streams and cardinality.
Series information can be queried through the [Series API](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/), or you can use [logcli](../../../query/).

In Loki 1.6.0 and newer the logcli series command added the `--analyze-labels` flag specifically for debugging high cardinality labels:

```
Total Streams:  25017
Unique Labels:  8

Label Name  Unique Values  Found In Streams
requestId   24653          24979
logStream   1194           25016
logGroup    140            25016
accountId   13             25016
logger      1              25017
source      1              25016
transport   1              25017
format      1              25017
```

In this example you can see the `requestId` label had a 24653 different values out of 24979 streams it was found in, this is bad!!

This is a perfect example of something which should not be a label, `requestId` should be removed as a label and instead
filter expressions should be used to query logs for a specific `requestId`. For example if `requestId` is found in
the log line as a key=value pair you could write a query like this: `{logGroup="group1"} |= "requestId=32422355"`

