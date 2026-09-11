---
title: Manage large volume log streams with automatic stream sharding
menuTitle: Automatic stream sharding
description: Describes how to control issues around the per-stream rate limit using automatic stream sharding.
weight: 
---

# Manage large volume log streams with automatic stream sharding

Automatic stream sharding can keep streams under a `desired_rate` by adding new labels and values to
existing streams. When properly tuned, this can eliminate issues where log producers are rate limited due to the
per-stream rate limit.

Automatic stream sharding is enabled by default (`shard_streams.enabled` defaults to `true`). To tune it or confirm it's on:

1. Check or set the global [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) in the Loki configuration file:

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

   The `desired_rate` defaults to `1536KB` (1.5MB), which is below the default `per_stream_rate_limit` of `3MB`. This gives Loki some headroom to shard a stream before it hits the per-stream rate limit.

1. Optionally enable `time_sharding_enabled` if you need to ingest old, out-of-order logs, for example during a backfill:

   ```yaml
   limits_config:
     shard_streams:
       enabled: true
       time_sharding_enabled: true
       time_sharding_ignore_recent: 40m
   ```

   Time-based sharding adds a `__time_shard__` label to streams, splitting log entries into buckets based on their timestamp. Log entries with a timestamp newer than `time_sharding_ignore_recent` (40 minutes by default) are still ingested, but Loki does not add the `__time_shard__` label to them. This lets very old logs be ingested without triggering out-of-order errors. Refer to [How automatic stream sharding works](#how-automatic-stream-sharding-works) for more detail.

  {{< admonition type="note" >}}
  `time_sharding_enabled` will be replaced by `ingester_time_sharding`.
  {{< /admonition >}}

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

Loki supports two independent sharding mechanisms, rate-based sharding and time-based sharding. You can enable either one on its own, or enable both together.

### Rate-based sharding

Rate-based sharding works by adding a new label, `__stream_shard__`, to streams and incrementing its value to try
and keep all streams below a configured `desired_rate`.

The feature adds a new API to Ingesters that reports the size of all existing log streams. Once per second, Distributors
query the API to get a picture of all stream rates in the system. Distributors use the existing stream-rate data and a
configured `desired_rate` to determine how many shards a given stream should have. The desired number of new log streams
are created with the label `__stream_shard__` and logs are divided evenly among the streams.

The once-per-second query interval is a Distributor setting, not a `shard_streams` limit. Use the `-distributor.rate-store.stream-rate-update-interval` flag, or the equivalent `rate_store` block in the Distributor configuration, to change how often Distributors refresh their view of stream rates.

Because rate-based sharding is reactive and relies on successive calls to Ingesters, the view of current rates is
always somewhat behind. As a result, the actual size of sharded streams will always be higher than the `desired_rate`.
In practice, this is still sufficient to keep log producers from being rate limited by per-stream rate limits.

### Time-based sharding

Time-based sharding works by adding a different label, `__time_shard__`, to streams. Loki calculates the value of this label from each log entry's timestamp, divided into buckets of `max_chunk_age`/2. This lets Loki accept out-of-order log entries that are older than would normally be allowed, because each time bucket becomes its own stream instead of extending an existing one.

Loki does not apply time-based sharding to log entries with a timestamp newer than the configured `time_sharding_ignore_recent` value. Those entries are still ingested, but they keep their original labels, so that recent logs are not needlessly split into extra streams.

If you enable both rate-based sharding and time-based sharding, Loki applies time-based sharding first, and then applies rate-based sharding to each of the resulting time-sharded streams.

### Ingester-side time-bucketing (experimental)

As an alternative to the distributor's `shard_streams.time_sharding_enabled`, Loki can instead let the Ingester itself
keep several time-bucketed chunks open at once for a single logical stream, rather than the distributor splitting a
stream into separate `__time_shard__` streams. With this mechanism, old and new entries for a stream always keep the
stream's original labels and land on the same Ingesters, and are not counted as extra streams against
`max_streams_per_user`/`max_global_streams_per_user`.

This is a separate, independently configured mechanism and does not replace `shard_streams.time_sharding_enabled` yet.
Enable only one of the two for a given tenant; running both at once is not currently prevented, but is not a supported
combination.

```yaml
limits_config:
  ingester_time_sharding:
    enabled: true
    ignore_recent: 40m
    max_open_buckets: 16
```

- `ignore_recent` behaves the same as `shard_streams.time_sharding_ignore_recent`: entries newer than this are always
  appended to the stream's current chunk.
- `max_open_buckets` bounds how many distinct historical time-buckets a single stream can have open at once, to cap
  the worst-case memory impact of a single push that spans a very wide time range. Entries that would open a bucket
  beyond this limit are discarded with reason `too_many_time_shard_buckets`.

Because bucketed chunks keep the stream's original labels, no `__time_shard__` label appears in query results, and
`/labels`/`/series` output is unaffected.

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

Use these metrics to observe ingester-side time-bucketing (`ingester_time_sharding`):

- `loki_ingester_stream_open_time_buckets`: The total number of open ingester-side time-shard buckets across all
  streams handled by this Ingester.
- `loki_ingester_time_sharded_samples_total` / `loki_ingester_time_sharded_bytes_total`: The total number of samples
  and bytes routed to an ingester-side time-shard bucket rather than a stream's live chunk, per tenant.
