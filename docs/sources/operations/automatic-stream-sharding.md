---
title: Manage large volume log streams with automatic stream sharding
menuTitle: Automatic stream sharding
description: Describes how to control issues around the per-stream rate limit using automatic stream sharding, including time-based sharding for out-of-order log ingestion.
weight: 
---

# Manage large volume log streams with automatic stream sharding

Automatic stream sharding can keep streams under a `desired_rate` by adding new labels and values to
existing streams. When properly tuned, this can eliminate issues where log producers are rate limited due to the
per-stream rate limit.

**To enable automatic stream sharding:**
1. Edit the global [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) of the Loki configuration file:

   ```yaml
   limits_config:
     shard_streams:
         enabled: true
   ```

1. Optionally lower the `desired_rate` in bytes if you find that the system is still hitting the `per_stream_rate_limit`:

   ```yaml
   limits_config:
     shard_streams:
       enabled: true
       desired_rate: 2097152 #2MiB
   ```

1. Optionally enable `logging_enabled` for debugging stream sharding.
  {{< admonition type="note" >}}
  This may affect the ingestion performance of Loki.
  {{< /admonition >}}

   ```yaml
   limits_config:
     shard_streams:
       enabled: true
       logging_enabled: true
   ```

## When to use automatic stream sharding

Large log streams present several problems for Loki, namely increased and uneven resource usage on Ingesters and
Distributors. The general recommendation is to explore existing log streams for additional label values that are both
useful for querying and sufficiently low cardinality. There are many cases, however, where no more labels can
be extracted, or cardinality for a label is dangerously large. To protect itself from such volume leading to operational failure, Loki implements per-stream rate limits;
but the result is that some data is lost. The per-stream limit also needs human intervention to change, which is not ideal when log volumes increase and decrease.

Loki uses automatic stream sharding to avoid rate limiting and large streams for any log stream by ensuring it is close
to a configured `desired_rate`.

## How automatic stream sharding works

Automatic stream sharding works by adding a new label, `__stream_shard__`, to streams and incrementing its value to try
and keep all streams below a configured `desired_rate`.

The feature adds a new API to Ingesters that reports the size of all existing log streams. Once per second, Distributors
query the API to get a picture of all stream rates in the system. Distributors use the existing stream-rate data and a
configured `desired_rate` to determine how many shards a given stream should have. The desired number of new log streams
are created with the label `__stream_shard__` and logs are divided evenly among the streams.

Because automatic stream sharding is reactive and relies on successive calls to Ingesters, the view of current rates is
always somewhat behind. As a result, the actual size of sharded streams will always be higher than the `desired_rate`.
In practice, this is still sufficient to keep log producers from being rate limited by per-stream rate limits.

## Time-based sharding

Time-based sharding addresses a different problem from rate-based sharding: it allows **out-of-order ingestion of logs with timestamps spread across multiple time windows**.

This is useful when logs are batched before being pushed to Loki — for example, when a mobile application concatenates log files client-side and uploads them later, or when an event-driven pipeline processes objects from object storage that contain logs spanning multiple days. In these cases, a single push request may contain log lines with timestamps hours or days apart, which Loki would normally reject or discard as out-of-order.

### How time-based sharding works

When `time_sharding_enabled` is set to `true`, Loki adds a `__time_shard__` label to each log line. The value of this label is derived from the log line's timestamp divided into buckets of `MaxChunkAge / 2`. This separates logs from different time windows into distinct streams, allowing Loki to accept and store them correctly without treating them as out-of-order within a single stream.

When both `time_sharding_enabled` and `enabled` (rate-based sharding) are set to `true`, time-based sharding is applied first.

### Enabling time-based sharding

1. Edit the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) section of the Loki configuration file:

   ```yaml
   limits_config:
     shard_streams:
       time_sharding_enabled: true
   ```

1. Optionally configure `time_sharding_ignore_recent` to prevent recent logs from being time-sharded. Logs with timestamps newer than this threshold are ingested normally without a `__time_shard__` label. The default is `40m`:

   ```yaml
   limits_config:
     shard_streams:
       time_sharding_enabled: true
       time_sharding_ignore_recent: 40m
   ```

   {{< admonition type="note" >}}
   Setting `time_sharding_ignore_recent` to `0` disables the recency exemption — all logs, including recent ones, will be time-sharded.
   {{< /admonition >}}

1. To enable both time-based and rate-based sharding simultaneously:

   ```yaml
   limits_config:
     shard_streams:
       enabled: true
       time_sharding_enabled: true
       time_sharding_ignore_recent: 40m
   ```

   Time-based sharding is applied before rate-based sharding when both are enabled.

## Automatic stream sharding metrics

Use these metrics to help tune Loki so that it is sharding streams aggressively enough to avoid the per-stream rate
limit:

- `loki_rate_store_refresh_failures_total`: The total number of failed attempts to refresh the distributor's view of
  stream rates.
- `loki_rate_store_streams`: The number of unique streams reported by all Ingesters. Sharded streams are reported as if
  they were unsharded.
- `loki_rate_store_max_stream_shards`: The maximum number of shards for any tenant of the system.
- `loki_rate_store_stream_shards`: A histogram of the distribution of shard counts across all streams.
- `loki_rate_store_max_stream_rate_bytes`: The maximum stream size in bytes/second for any tenant of the system. Sharded
  streams are reported as if they are unsharded.
- `loki_rate_store_max_unique_stream_rate_bytes`: The maximum size of any stream across all tenants. Stream shards are
  individually reported.
- `loki_rate_store_stream_rate_bytes`: A histogram of the distribution of stream sizes across all tenants in
  bytes/second.
- `loki_stream_sharding_count`: The total number of times that streams have been sharded. Useful for calculating the
  sharding rate.
