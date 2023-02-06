---
title: Automatic stream sharding
menuTitle: Automatic stream sharding
description: Automatic stream sharding can control issues around the per-stream rate limit
weight: 110
---

# Automatic stream sharding

Automatic stream sharding will attempt to keep streams under a `desired_rate` by adding new labels and values to
existing streams. When properly tuned, this should eliminate issues where log producers are rate limited due to the
per-stream rate limit.

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

## Enabling and configuring automatic stream sharding

Enable automatic sharding by setting the global or per-tenant override `shard_streams`. This configuration contains
an `enabled` flag to turn the feature on, a `desired_rate` configuration for the desired stream rate, and an
optional `logging_enabled` flag to enable debug logging of stream sharding.

*NOTE*: Setting `logging_enabled` may affect the ingestion performance of Loki.

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
