---
title: Current best practices
---
# Loki label best practices

Loki is under active development, and we are constantly working to improve performance. But here are some of the most current best practices for labels that will give you the best experience with Loki.

## 1. Static labels are good

Things like, host, application, and environment are great labels. They will be fixed for a given system/app and have bounded values. Use static labels to make it easier to query your logs in a logical sense (e.g. show me all the logs for a given application and specific environment, or show me all the logs for all the apps on a specific host).

## 2. Use dynamic labels sparingly

Too many label value combinations leads to too many streams. The penalties for that in Loki are a large index and small chunks in the store, which in turn can actually reduce performance.

To avoid those issues, don't add a label for something until you know you need it! Use filter expressions ( |= “text”, |~ “regex”, …) and brute force those logs. It works -- and it's fast.

From early on, we have set a label dynamically using promtail pipelines for `level`. This seemed intuitive for us as we often wanted to only show logs for `level=”error”`; however, we are re-evaluating this now as writing a query. `{app=”loki”} |= “level=error”` is proving to be just as fast for many of our applications as `{app=”loki”,level=”error”}`.

This may seem surprising, but if applications have medium to low volume, that label causes one application's logs to be split into up to five streams, which means 5x chunks being stored.  And loading chunks has an overhead associated with it. Imagine now if that query were `{app=”loki”,level!=”debug”}`. That would have to load **way** more chunks than `{app=”loki”} != “level=debug”`.

Above, we mentioned not to add labels until you _need_ them, so when would you _need_ labels?? A little farther down is a section on `chunk_target_size`. If you set this to 1MB (which is reasonable), this will try to cut chunks at 1MB compressed size, which is about 5MB-ish of uncompressed logs (might be as much as 10MB depending on compression). If your logs have sufficient volume to write 5MB in less time than `max_chunk_age`, or **many** chunks in that timeframe, you might want to consider splitting it into separate streams with a dynamic label.

What you want to avoid is splitting a log file into streams, which result in chunks getting flushed because the stream is idle or hits the max age before being full. As of [Loki 1.4.0](https://grafana.com/blog/2020/04/01/loki-v1.4.0-released-with-query-statistics-and-up-to-300x-regex-optimization/), there is a metric which can help you understand why chunks are flushed `sum by (reason) (rate(loki_ingester_chunks_flushed_total{cluster="dev"}[1m]))`.

It’s not critical that every chunk be full when flushed, but it will improve many aspects of operation. As such, our current guidance here is to avoid dynamic labels as much as possible and instead favor filter expressions. For example, don’t add a `level` dynamic label, just `|= “level=debug”` instead.

## 3. Label values must always be bounded

If you are dynamically setting labels, never use a label which can have unbounded or infinite values. This will always result in big problems for Loki.

Try to keep values bounded to as small a set as possible. We don't have perfect guidance as to what Loki can handle, but think single digits, or maybe 10’s of values for a dynamic label. This is less critical for static labels. For example, if you have 1,000 hosts in your environment it's going to be just fine to have a host label with 1,000 values.

## 4. Be aware of dynamic labels applied by clients

Loki has several client options: [Promtail](https://github.com/grafana/loki/tree/master/docs/clients/promtail) (which also supports systemd journal ingestion and TCP-based syslog ingestion), [Fluentd](https://github.com/grafana/loki/tree/master/fluentd/fluent-plugin-grafana-loki), [Fluent Bit](https://github.com/grafana/loki/tree/master/cmd/fluent-bit), a [Docker plugin](https://grafana.com/blog/2019/07/15/lokis-path-to-ga-docker-logging-driver-plugin-support-for-systemd/), and more!

Each of these come with ways to configure what labels are applied to create log streams. But be aware of what dynamic labels might be applied. 
Use the Loki series API to get an idea of what your log streams look like and see if there might be ways to reduce streams and cardinality. 
Details of the Series API can be found [here](https://grafana.com/docs/loki/latest/api/#series), or you can use [logcli](https://grafana.com/docs/loki/latest/getting-started/logcli/) to query Loki for series information.

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

## 5. Configure caching

Loki can cache data at many levels, which can drastically improve performance. Details of this will be in a future post.

## 6. Logs must be in increasing time order per stream

One issue many people have with Loki is their client receiving errors for out of order log entries.  This happens because of this hard and fast rule within Loki:

- For any single log stream, logs must always be sent in increasing time order. If a log is received with a timestamp older than the most recent log received for that stream, that log will be dropped.

There are a few things to dissect from that statement. The first is this restriction is per stream.  Let’s look at an example:

```
{job=”syslog”} 00:00:00 i’m a syslog!
{job=”syslog”} 00:00:01 i’m a syslog!
```

If Loki received these two lines which are for the same stream, everything would be fine. But what about this case:

```
{job=”syslog”} 00:00:00 i’m a syslog!
{job=”syslog”} 00:00:02 i’m a syslog!
{job=”syslog”} 00:00:01 i’m a syslog!  <- Rejected out of order!
```

What can we do about this? What if this was because the sources of these logs were different systems? We can solve this with an additional label which is unique per system:

```
{job=”syslog”, instance=”host1”} 00:00:00 i’m a syslog!
{job=”syslog”, instance=”host1”} 00:00:02 i’m a syslog!
{job=”syslog”, instance=”host2”} 00:00:01 i’m a syslog!  <- Accepted, this is a new stream!
{job=”syslog”, instance=”host1”} 00:00:03 i’m a syslog!  <- Accepted, still in order for stream 1
{job=”syslog”, instance=”host2”} 00:00:02 i’m a syslog!  <- Accepted, still in order for stream 2
```

But what if the application itself generated logs that were out of order? Well, I'm afraid this is a problem. If you are extracting the timestamp from the log line with something like [the promtail pipeline stage](https://grafana.com/docs/loki/latest/clients/promtail/stages/timestamp/), you could instead _not_ do this and let Promtail assign a timestamp to the log lines. Or you can hopefully fix it in the application itself.

But I want Loki to fix this! Why can’t you buffer streams and re-order them for me?! To be honest, because this would add a lot of memory overhead and complication to Loki, and as has been a common thread in this post, we want Loki to be simple and cost-effective. Ideally we would want to improve our clients to do some basic buffering and sorting as this seems a better place to solve this problem.

It's also worth noting that the batching nature of the Loki push API can lead to some instances of out of order errors being received which are really false positives. (Perhaps a batch partially succeeded and was present; or anything that previously succeeded would return an out of order entry; or anything new would be accepted.)

## 7. Use `chunk_target_size`

This was added earlier this year when we [released v1.3.0 of Loki](https://grafana.com/blog/2020/01/22/loki-1.3.0-released/), and we've been experimenting with it for several months. We have `chunk_target_size: 1536000` in all our environments now. This instructs Loki to try to fill all chunks to a target _compressed_ size of 1.5MB. These larger chunks are more efficient for Loki to process.

A couple other config variables affect how full a chunk can get. Loki has a default `max_chunk_age` of 1h and `chunk_idle_period` of 30m to limit the amount of memory used as well as the exposure of lost logs if the process crashes.

Depending on the compression used (we have been using snappy which has less compressibility but faster performance), you need 5-10x or 7.5-10MB of raw log data to fill a 1.5MB chunk. Remembering that a chunk is per stream, the more streams you break up your log files into, the more chunks that sit in memory, and the higher likelihood they get flushed by hitting one of those timeouts mentioned above before they are filled.

Lots of small, unfilled chunks are currently kryptonite for Loki. We are always working to improve this and may consider a compactor to improve this in some situations. But, in general, the guidance should stay about the same: Try your best to fill chunks!

If you have an application that can log fast enough to fill these chunks quickly (much less than `max_chunk_age`), then it becomes more reasonable to use dynamic labels to break that up into separate streams.

## 8. Use `-print-config-stderr` or `-log-config-reverse-order`

Starting in version 1.6.0 Loki and Promtail have flags which will dump the entire config object to stderr, or the log file, when they start.

`-print-config-stderr` is nice when running loki directly e.g. `./loki ` as you can get a quick output of the entire Loki config.

`-log-config-reverse-order` is the flag we run Loki with in all our environments, the config entries are reversed so that the order of configs reads correctly top to bottom when viewed in Grafana's Explore.
