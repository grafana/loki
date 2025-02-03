---
title: Configuration best practices
menuTitle:  Best practices
description: Describes configuration best practices for Grafana Loki.
weight:  100
---
# Configuration best practices

Grafana Loki is under active development, and the Loki team is constantly working to improve performance. But here are some of the most current best practices for configuration that will give you the best experience with Loki.

## Configure caching

Loki can cache data at many levels, which can drastically improve performance. Details of this will be in a future post.

## Time ordering of logs

Loki [accepts out-of-order writes](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#accept-out-of-order-writes) _by default_.
This section identifies best practices when Loki is _not_ configured to accept out-of-order writes.

One issue many people have with Loki is their client receiving errors for out of order log entries.  This happens because of this hard and fast rule within Loki:

- For any single log stream, logs must always be sent in increasing time order. If a log is received with a timestamp older than the most recent log received for that stream, that log will be dropped.

There are a few things to dissect from that statement. The first is this restriction is per stream.  Let’s look at an example:

```bash
{job="syslog"} 00:00:00 i'm a syslog!
{job="syslog"} 00:00:01 i'm a syslog!
```

If Loki received these two lines which are for the same stream, everything would be fine. But what about this case:

```bash
{job="syslog"} 00:00:00 i'm a syslog!
{job="syslog"} 00:00:02 i'm a syslog!
{job="syslog"} 00:00:01 i'm a syslog!  <- Rejected out of order!
```

What can you do about this? What if this was because the sources of these logs were different systems? You can solve this with an additional label which is unique per system:

```bash
{job="syslog", instance="host1"} 00:00:00 i'm a syslog!
{job="syslog", instance="host1"} 00:00:02 i'm a syslog!
{job="syslog", instance="host2"} 00:00:01 i'm a syslog!  <- Accepted, this is a new stream!
{job="syslog", instance="host1"} 00:00:03 i'm a syslog!  <- Accepted, still in order for stream 1
{job="syslog", instance="host2"} 00:00:02 i'm a syslog!  <- Accepted, still in order for stream 2
```

But what if the application itself generated logs that were out of order? Well, I'm afraid this is a problem. If you are extracting the timestamp from the log line with something like [the Promtail pipeline stage](/docs/loki/<LOKI_VERSION>/send-data/promtail/stages/timestamp/), you could instead _not_ do this and let Promtail assign a timestamp to the log lines. Or you can hopefully fix it in the application itself.

It's also worth noting that the batching nature of the Loki push API can lead to some instances of out of order errors being received which are really false positives. (Perhaps a batch partially succeeded and was present; or anything that previously succeeded would return an out of order entry; or anything new would be accepted.)

## Use `snappy` compression algorithm

`Snappy` is currently the Loki compression algorithm of choice. It performs much better than `gzip` for speed, but it is not as efficient in storage. This was an acceptable tradeoff for us.

Grafana Labs has found that `gzip` was very good for compression but was very slow, and this was causing slow query responses.

`LZ4` is a good compromise of speed and compression performance. While compression is slightly slower than `snappy`, the compression ratio is higher, resulting in smaller chunks in object storage.

## Use `chunk_target_size`

Using `chunk_target_size` instructs Loki to try to fill all chunks to a target _compressed_ size of 1.5MB. These larger chunks are more efficient for Loki to process.

Other configuration variables affect how full a chunk can get. Loki has a default `max_chunk_age` of 2h and `chunk_idle_period` of 30m to limit the amount of memory used as well as the exposure of lost logs if the process crashes.

Depending on the compression used (Loki has been using snappy which has less compressibility but faster performance), you need 5-10x or 7.5-10MB of raw log data to fill a 1.5MB chunk. Remembering that a chunk is per stream, the more streams you break up your log files into, the more chunks that sit in memory, and the higher likelihood they get flushed by hitting one of those timeouts mentioned above before they are filled.

Lots of small, unfilled chunks negatively affect Loki. The team is always working to improve this and may consider a compactor to improve this in some situations. But, in general, the guidance should stay about the same: try your best to fill chunks.

If you have an application that can log fast enough to fill these chunks quickly (much less than `max_chunk_age`), then it becomes more reasonable to use dynamic labels to break that up into separate streams.

## Use `-print-config-stderr` or `-log-config-reverse-order`

Loki and Promtail have flags which will dump the entire config object to stderr or the log file when they start.

`-print-config-stderr` works well when invoking Loki from the command line, as you can get a quick output of the entire Loki configuration.

`-log-config-reverse-order` is the flag Grafana runs Loki with in all our environments. The configuration entries are reversed, so that the order of the configuration reads correctly top to bottom when viewed in Grafana's Explore.
