---
title: Understanding Loki configuration limits
menuTitle: Configuration limits
description: Learn about Loki configuration limits and why respecting them ensures cluster stability and optimal performance.
weight: 100
---

# Understanding Loki configuration limits

Loki implements configuration limits to maintain system stability, ensure fair resource distribution, and protect performance across all users. You can adjust many limits for specific use cases. Other limits are hard boundaries that you should not exceed, to prevent performance degradation and system instability.

This topic explains the critical configuration limits in Loki, why they exist, and the consequences of exceeding them.

## Why limits exist

Configuration limits in Loki serve multiple essential purposes:

* **System stability:** Limits prevent individual queries or ingestion patterns from exhausting system resources. Without limits, a single tenant or misconfigured application could consume all available memory, CPU, or I/O (input/output) capacity. This can cause cascading failures across the entire system.

* **Performance protection:** Loki is optimized for specific usage patterns. Limits ensure that workloads stay within the parameters where Loki performs efficiently. Exceeding these limits often results in exponential performance degradation rather than linear scaling.

* **Fair resource allocation:** In multi-tenant environments, limits ensure that no single tenant can monopolize shared resources. Query workers, ingester memory, and storage I/O must be distributed fairly across all users.

* **Operational predictability:** Limits allow operators to plan capacity and scale infrastructure appropriately. When operators understand the boundaries of expected workloads, they can provision resources that meet service level agreement (SLA) requirements.

Exceeding a limit doesn't just affect the tenant who exceeded it. It can also impact overall system performance and stability, often affecting all tenants in a shared environment.

## Default ingest limits

The following limits apply when you push logs to Loki. These are the default values in the Loki open source release. If a request exceeds one of these limits, Loki rejects the affected log lines or streams and returns an error.

| Limit | Default value | What happens when you exceed it |
| --- | --- | --- |
| Maximum active streams per tenant (`max_global_streams_per_user`) | 5,000 | New streams are rejected (`stream_limit`) until existing streams age out. |
| Ingestion rate (`ingestion_rate_mb`) | 4 MB per second, per tenant | Requests are rejected and can be retried (`rate_limited`). |
| Ingestion burst size (`ingestion_burst_size_mb`) | 6 MB | Requests that exceed the burst allowance are rejected until the rate limit bucket refills. |
| Per-stream rate limit (`per_stream_rate_limit`) | 3 MB per second | The stream is rate limited (`per_stream_rate_limit`). |
| Per-stream burst limit (`per_stream_rate_limit_burst`) | 15 MB | Bursts that exceed this size are rejected. |
| Maximum log line size (`max_line_size`) | 256 KB | Log lines that exceed the limit are discarded (`line_too_long`), unless you configure Loki to truncate them instead. |
| Maximum labels per stream (`max_label_names_per_series`) | 15 | Streams that exceed the limit are rejected (`max_label_names_per_series`). |
| Maximum length of a label name (`max_length_label_name`) | 1,024 bytes | Label names that exceed the limit are rejected (`label_name_too_long`). |
| Maximum length of a label value (`max_length_label_value`) | 2,048 bytes | Label values that exceed the limit are rejected (`label_value_too_long`). |
| Maximum structured metadata entries per log line (`max_structured_metadata_entries_count`) | 128 entries | Log lines with more entries are discarded. |
| Maximum structured metadata size per log line (`max_structured_metadata_size`) | 64 KB | Log lines that exceed the limit are discarded. |
| Maximum accepted sample age (`reject_old_samples_max_age`) | 1 week | Samples older than this age are rejected (`greater_than_max_sample_age`). |

Loki rejects samples based on their age only when the `reject_old_samples` setting is `true`, which is the default. If you set `reject_old_samples` to `false`, Loki ignores `reject_old_samples_max_age`.

Ingestion rate and maximum active streams aren't fixed for every deployment. Many operators raise these limits for larger deployments. For guidance on how far you can safely raise them, see [Limits we strongly recommend not exceeding](#limits-we-strongly-recommend-not-exceeding).

## Default query limits

The following limits apply when you query logs in Loki. These are the default values in the Loki open source release.

| Limit | Default value | What happens when you exceed it |
| --- | --- | --- |
| Query timeout (`query_timeout`) | 1 minute | The query is terminated before it finishes. |
| Maximum log entries returned per query (`max_entries_limit_per_query`) | 5,000 | Queries that request more entries than this limit are rejected. |
| Maximum unique series returned by a metric query (`max_query_series`) | 500 | The query is rejected. |
| Maximum query parallelism (`max_query_parallelism`) | 32 | Additional subqueries wait in a queue until a query worker is free. |
| Maximum query time range (`max_query_length`, `store.max-query-length`) | 721 hours (30 days and 1 hour) | The query is rejected. |
| Maximum chunks fetched per query (`store.query-chunk-limit`) | 2,000,000 | The query is rejected. |
| Maximum stream matchers per query (`max_streams_matcher_per_query`) | 1,000 | The query is rejected. |
| Maximum concurrent tail requests (`max_concurrent_tail_requests`) | 10 | Additional tail requests are rejected. |

## Limits we strongly recommend not exceeding

Some limits can technically be raised well beyond their defaults, but doing so puts your Loki cluster at risk. This section explains which limits fall into that category, why they exist, and what to do instead of raising them further.

### ingestion_rate_mb

- **Default**: 4 MB per second
- **Example large deployment value**: 100 MB per second (approximately 260 TB of data per month)
- **Recommended maximum for large deployments**: up to 250 MB per second

This limit controls the per-tenant ingestion rate in megabytes per second.

**Why this limit matters**:

Ingestion rate limits protect against cascading failures:

- Sudden traffic spikes can overwhelm ingesters before your infrastructure can scale to meet demand.
- Unbounded ingestion can exhaust ingester memory.
- Unbounded ingestion can exhaust distributor memory.
- Network and storage I/O can become bottlenecks.

Rate limits provide a safety mechanism that allows the system to scale gracefully, rather than failing all at once.

**Consequences of not setting rate limits properly**:

- Cascading failure of distributors and ingesters, leading to an outage of the entire write path and the recent-data read path.
- Ingesters overwhelmed to the point that their write-ahead logs (WALs) run out of disk space, causing slow WAL replay and difficult recovery.
- Object storage overwhelmed by excessive requests.

**What to do instead**:

- Always set rate limits for every tenant.
- Implement exponential backoff in your log shipping clients.
- Monitor ingestion patterns and adjust limits proactively.
- Scale infrastructure before increasing limits significantly.

### max_global_streams_per_user

- **Default**: 5,000
- **Recommended maximum for large deployments**: 80,000
- **Hard limit**: approximately 200,000 (performance issues become unavoidable beyond this point)

This limit controls the maximum number of active streams a single tenant can have across all ingesters. Each unique combination of labels creates a separate stream.

**Why you should not exceed 200,000 streams**:

Streams consume resources throughout the system:

- **Ingester memory**: Each stream maintains in-memory chunks.
- **Index entries**: Each stream creates entries in the index.
- **Object storage operations**: Each stream generates flush operations.
- **Query overhead**: More streams means more chunks to scan during queries.

Beyond approximately 200,000 streams, these costs become prohibitive regardless of infrastructure size.

**Consequences of exceeding**:

- Ingester memory exhaustion and crashes.
- Extremely slow query performance.
- Index query timeouts.
- Excessive object storage operations and costs.
- Inability to meet performance SLAs.
- Potential data loss during ingester failures.

**What to do instead**:

- Audit your label usage to identify high-cardinality labels.
- Use static labels for source identification only.
- Move high-cardinality data to structured metadata.
- Calculate your expected stream count: `streams = label1_values × label2_values × ...`
- Design for fewer than 80,000 streams.

### max_label_names_per_series

- **Default**: 15
- **Recommended maximum**: 15 (do not exceed)

This limit controls the maximum number of labels you can attach to a single log stream.

**Why you should not exceed 15 labels**:

You should be able to identify and categorize your logs with fewer than 15 labels. More labels increase the likelihood of higher cardinality and cause logs to fragment into too many streams and chunks. Even labels with a small, fixed set of values still require space in the index, and each additional label makes the index larger and can slow down index operations. Always use as few labels as possible.

Having more than 15 labels per stream usually indicates a labeling problem, not a limit you need to raise. Loki is designed for a small set of labels that describe the log source, such as `cluster`, `namespace`, or `job`. Labels aren't meant for storing high-cardinality metadata.

**Consequences of exceeding**:

- Increased memory usage on ingesters.
- A larger index, which leads to slower index operations and slower queries.

**What to do instead**:

- Use labels only for low-cardinality source identification.
- Parse high-cardinality fields at query time using LogQL parsers.
- Store high-cardinality metadata in [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) instead of labels.

### max_line_size

- **Default**: 256 KB
- **Maximum recommended**: 256 KB (do not exceed)

This limit controls the maximum size, in bytes, of a single log line during ingestion.

**Why you should not exceed 256 KB**:

A limit on log line size is fundamental to Loki's design. Increasing this limit can have several unintended consequences that make it hard to run a stable Loki cluster:

- Chunks are expected to be a few MB at most. If a log line is larger than this, it can create very large chunks, which hurts query performance because a chunk is the unit of parallelism and is expected to stay around 2 MB or smaller.
- Queriers and query frontends become much more likely to run out of memory (OOM).
- Increasing this limit also increases the risk of exhausting the gRPC message size limit, which can affect the performance of other tenants.

**What to do instead**:

- Structure your applications to produce reasonably sized log lines.
- Avoid embedding large payloads (such as base64-encoded data or full HTTP bodies) in logs.
- Log references to external data instead of the data itself.
- Split multi-line events into separate log entries.
- Set `max_line_size_truncate` to truncate large log lines instead of dropping them. Note that truncation can break JSON parsing of large log lines.

### Out-of-order ingestion window

- **Default**: 1 hour (half of `max_chunk_age`, which defaults to 2 hours)
- **Adjustable**: No, not on a per-tenant basis

The out-of-order ingestion window controls how far back in time, relative to the newest entry already received for a stream, Loki accepts a new log entry. Loki calculates this window as `max_chunk_age / 2`, so with the default 2-hour `max_chunk_age`, Loki accepts entries up to 1 hour older than the current newest entry in each stream.

This window isn't a per-tenant override. It's a cluster-wide setting, because it derives from `max_chunk_age`, which affects chunk flushing behavior across the whole deployment.

**Why this limit exists**:

The reasons users hit this limit vary widely: misconfiguration, re-ingestion of old data, or logs from temporarily disconnected sources such as vehicles or IoT devices. Loki needs a bounded window so that ingesters know when a chunk is complete and safe to flush.

**What to do for historical data**:

If you need to ingest logs older than the out-of-order window allows, you have two options:

- Temporarily increase `reject_old_samples_max_age` during migrations or backfills, so Loki doesn't reject the old samples outright.
- Enable `shard_streams.time_sharding_enabled` for the affected tenant. This setting splits incoming streams by injecting a synthetic `__time_shard__` label, so that no stream is ever longer than `max_chunk_age / 2`. This lets Loki accept very old logs, because the new, shorter streams are never "too far behind" by design.

Time sharding has some limitations:

- Newly ingested old logs aren't queryable for a few hours, until Loki flushes them to object storage.
- Very old logs can still be rejected with a `greater_than_max_sample_age` error if they fall outside `reject_old_samples_max_age`.
- Recording rules don't work against the old data.
- Time sharding creates new time series (up to 24 per day), so check whether this affects your active streams limit.

Disable time sharding after your backfill completes.

### per_stream_rate_limit

- **Default**: 3 MB per second sustained, 15 MB burst
- **Recommended maximum sustained rate**: 5 MB per second

Individual streams have their own rate limits. This prevents a single high-volume stream from overwhelming the ingesters it is distributed to, based on the replication factor.

**Why you should not exceed 5 MB per second per stream**:

Streams are distributed to a fixed number of ingesters (typically 3, for replication). If a single stream sends data faster than those ingesters can process it:

- Ingester memory queues grow.
- Chunk flush operations lag behind incoming data.
- Memory usage spikes on the affected ingesters.
- The affected ingesters risk running out of memory (OOM).

Unlike the global rate limit, you cannot solve per-stream limit problems by adding more ingesters, because each stream is always handled by the same set of ingesters.

**Consequences of exceeding**:

- Memory buildup on specific ingesters.
- Increased flush latency.
- Potential out-of-memory (OOM) failures on affected ingesters.
- Data loss if an ingester crashes.
- Uneven load distribution.

**What to do instead**:

- First, try adjusting automatic stream sharding by lowering `desired_rate` (not below 128 KB per second).
- If that isn't enough, increase `per_stream_rate_limit` and `per_stream_rate_limit_burst`. This might require more memory headroom on your ingesters. Increase the burst limit more aggressively than the sustained rate.

For example, to handle "bursty" streams, you could raise the sustained rate a little and the burst limit a lot:

- `per_stream_rate_limit: 5MB/s`
- `per_stream_rate_limit_burst: 30MB/s`

This gives each stream a bucket that holds 30 MB. A 30 MB push drains the entire bucket, which then refills at 5 MB per second up to the 30 MB cap. You could raise the burst limit further, to 60 MB, to handle spikier streams, but avoid raising the sustained rate too far, since that keeps overall memory use predictable. Automatic stream sharding also continuously works to keep stream volumes near `desired_rate`, but it can't always react fast enough to very sudden changes.

### query_timeout

- **Default**: 1 minute
- **Recommended maximum**: 5 minutes (do not exceed)

The query timeout controls how long a query can run before Loki terminates it. This limit exists because Loki is designed as a synchronous query engine, not for very long-running queries.

**Why you should not exceed 5 minutes**:

Loki was not engineered for very long-running query operations. The way Loki queries work, each querier runs a fixed number of worker routines. Query frontends and schedulers split a query by time range or by label set and enqueue each resulting subquery, and a single worker routine executes each one. All tenants share this pool of query workers.

Raising the timeout for one tenant lets that tenant reserve query workers for longer, which reduces the query capacity available to every other tenant. In multi-tenant or large multi-user environments, this can make it difficult to maintain system stability.

**Consequences of exceeding**:

- Reduced query capacity for all tenants.
- Increased query queue wait times.
- Potential denial of service to other users.
- Unpredictable query performance.

**What to do instead**: Optimize your queries to complete faster. Use smaller time ranges, more specific labels, and filters applied as early as possible in the query.

## Understanding limit violations

When you exceed a limit, Loki provides metrics and errors to help you understand what happened.

### Monitoring metrics

- **`loki_discarded_samples_total`**: Counts log lines discarded, with a `reason` label indicating which limit was exceeded.
- **`loki_discarded_bytes_total`**: Measures bytes discarded, by reason.
- **`loki_ingester_memory_streams`**: Current number of active streams in memory, per tenant.

### Common error reasons

- **`rate_limited`**: Exceeded `ingestion_rate_mb`.
- **`per_stream_rate_limit`**: An individual stream exceeded its rate limit.
- **`stream_limit`**: Exceeded `max_global_streams_per_user`.
- **`line_too_long`**: A log line exceeded `max_line_size`.
- **`max_label_names_per_series`**: Too many labels on a stream.
- **`label_name_too_long`**: A label name exceeded `max_length_label_name`.
- **`label_value_too_long`**: A label value exceeded `max_length_label_value`.
- **`too_far_behind`**: A log entry fell outside the out-of-order ingestion window.
- **`greater_than_max_sample_age`**: A log entry is older than `reject_old_samples_max_age`.

## Best practices for limits

To work effectively within Loki's limits:

1. **Design for low cardinality**: Use 5 to 10 labels at most, with static values.
2. **Keep logs reasonably sized**: Target 1 to 10 KB per log line.
3. **Forward logs promptly**: Aim for sub-second delivery latency.
4. **Monitor your usage**: Set up alerts on discarded-sample metrics.
5. **Scale infrastructure first**: Increase resources before you increase limits.
6. **Use alternatives**: Use structured metadata and query-time parsing instead of more labels.
7. **Plan capacity**: Calculate your expected stream count before deployment.
8. **Test at scale**: Validate your label design with production-like data volumes.

## Next steps

To change any of these limits for your own deployment, see the `limits_config` block in the [Configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/), and review the related topics below before you raise a limit.

## Related documentation

- [Configure Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/bp-configure/) - Practical guide to configuring Loki
- [Rate limits and validation](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/request-validation-rate-limits/) - Detailed error handling
- [Understanding cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/) - How cardinality affects performance
- [Labels best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/bp-labels/) - Guide to label design
- [Structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) - Alternative to high-cardinality labels
