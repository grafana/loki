---
title: Understanding Loki configuration limits
menuTitle: Configuration limits
description: Learn about Loki configuration limits and why respecting them ensures cluster stability and optimal performance.
weight: 
---

# Understanding Loki configuration limits

Loki implements configuration limits to maintain system stability, ensure fair resource distribution, and protect performance across all users. While many limits can be adjusted for specific use cases, some limits represent hard boundaries that should not be exceeded to prevent performance degradation and system instability.

This topic explains the critical configuration limits in Loki, why they exist, and the consequences of exceeding them.

## Why limits exist

Configuration limits in Loki serve multiple essential purposes:

* **System stability:** Limits prevent individual queries or ingestion patterns from exhausting system resources. Without limits, a single tenant or misconfigured application could consume all available memory, CPU, or I/O capacity, causing cascading failures across the entire system.

* **Performance protection:** Loki is optimized for specific usage patterns. Limits ensure that workloads stay within the parameters where Loki performs efficiently. Exceeding these limits often results in exponential performance degradation rather than linear scaling.

* **Fair resource allocation:** In multi-tenant environments, limits ensure that no single tenant can monopolize shared resources. Query workers, ingester memory, and storage I/O must be distributed fairly across all users.

* **Operational predictability:**  Limits allow operators to capacity plan and scale infrastructure appropriately. By understanding the boundaries of expected workloads, operators can provision resources that meet SLA requirements.

The common theme is that exceeding limits doesn't just affect the user exceeding them—it impacts overall system performance and stability, often affecting all tenants in shared environments.

## Ingest limits

Ingest limits protect the ingestion path and prevent resource exhaustion in the ingesters and distributors.

### max_global_streams_per_user

- **Default**: 5,000
- **Large deployments**: 80,000
- **Hard limit**: ~200,000 (performance issues become unavoidable)

This limit controls the maximum number of active streams a single tenant can have across all ingesters. Each unique combination of labels creates a separate stream.

**Why you should not exceed 200,000 streams**:

Streams consume resources throughout the system:

- **Ingester memory**: Each stream maintains in-memory chunks
- **Index entries**: Each stream creates entries in the index
- **Object storage operations**: Each stream generates flush operations
- **Query overhead**: More streams means more chunks to scan during queries

Beyond ~200,000 streams, these costs become prohibitive regardless of infrastructure:

- Ingesters run out of memory even with maximum RAM
- Index queries become too slow
- Chunk flush operations overwhelm object storage
- Query performance becomes unacceptable

**Consequences of exceeding**:

- Ingester memory exhaustion and crashes
- Extremely slow query performance
- Index query timeouts
- Excessive object storage operations and costs
- Inability to meet performance SLAs
- Potential data loss during ingester failures

**What to do instead**:

- Audit your label usage to identify high-cardinality labels
- Use static labels for source identification only
- Move high-cardinality data to structured metadata
- Calculate expected stream count: `streams = label1_values × label2_values × ...`
- Design for fewer than 80,000 streams

### ingestion_rate_mb

- **Default**: 4 MB/s
- **Example**: 100 MB/s (which translates to approximately 260TB of data per month.)
- **Large deployments**: Up to 250 MB/s

This limit controls the per-tenant ingestion rate in megabytes per second.

**Why this limit exists**:

Ingestion rate limits protect against cascading failures:

- Sudden traffic spikes can overwhelm ingesters before autoscaling responds
- Unbounded ingestion can exhaust ingester memory
- High burst rates can cause distributor backpressure
- Network and storage I/O can become bottlenecks

Rate limits provide a safety mechanism that allows the system to scale gracefully rather than failing catastrophically.

**Consequences of exceeding**:

- Request rejections with HTTP 429 (Too Many Requests)
- Data loss if clients don't implement retry logic
- Backpressure on log shipping agents
- Potential cascading failures during infrastructure scaling
- Degraded performance for all tenants

**What to do instead**:

- Set rate limits with 50% headroom above peak usage
- Configure burst limits to handle short-term spikes
- Implement exponential backoff in log shipping clients
- Monitor ingestion patterns and adjust limits proactively
- Scale infrastructure before increasing limits significantly

### per_stream_rate_limit

- **Default**: 3 MB/s
- **Recommended maximum sustained**: 5 MB/s
- **Recommended maximum burst**: 20 MB

Individual streams have rate limits to prevent a single high-volume stream from overwhelming the ingesters it is distributed to based on replication factor.

**Why you should not exceed 5 MB/s per stream**:

Streams are distributed to a fixed number of ingesters (typically 3 for replication). If a single stream sends data faster than ingesters can process:

- Ingester memory queues grow
- Chunk flush operations lag behind incoming data
- Memory usage spikes on affected ingesters
- Risk of OOM on specific ingesters

Unlike the global rate limit, per-stream limits cannot be solved by adding more ingesters, as each stream is always handled by the same set of ingesters.

**Consequences of exceeding**:

- Memory buildup on specific ingesters
- Increased flush latency
- Potential OOM on affected ingesters
- Data loss if ingester crashes
- Uneven load distribution

**What to do instead**:

- Split high-volume streams by adding appropriate labels (such as instance)
- Use client-side rate limiting with Promtail's limit stage
- Sample verbose logs at the application level

## Out-of-order ingestion limits

### Out-of-order ingestion window

- **Default window**: 1 hour (controlled by `max_chunk_age / 2`)
- **Default `max_chunk_age`**: 2 hours
- **Configuration**: Global, not adjustable per tenant

Loki accepts out-of-order writes within a window of `max_chunk_age / 2` (typically 1 hour). Logs arriving outside this window are rejected.

**Why this limit exists**:

Loki optimizes for ordered, sequential log ingestion:

- Chunks are built sequentially in memory
- Out-of-order logs require complex chunk management
- Wide out-of-order windows increase memory pressure
- Index performance degrades with large time ranges per stream

The 1-hour window is calculated to balance accepting delayed logs while maintaining optimal chunk sizes and indexing performance.

**Why you should not increase `max_chunk_age`**:

`max_chunk_age` is a global configuration affecting all tenants:

- Increasing it increases memory usage on all ingesters
- Larger chunks take longer to flush
- Query performance degrades with larger chunks
- Higher risk of data loss on ingester failure
- Cannot be tuned per tenant

**Consequences of exceeding the window**:

- Log lines rejected with `too_far_behind` error
- Data loss if not handled by retry logic
- Gaps in log data

**What to do instead**:

- Configure log shippers to forward logs promptly
- Use proper buffering and retry logic in forwarding agents
- Ensure time synchronization across log sources
- For historical data ingestion, use time sharding feature temporarily

### reject_old_samples_max_age

- **Default**: 1 week
- **Adjustable**: Yes, per tenant

This setting rejects samples older than the specified age. With time sharding enabled for historical data ingestion, very old logs may still be rejected beyond this threshold.

**Why this limit exists**:

Accepting very old logs can:

- Complicate retention policies
- Create confusion about data freshness
- Impact compaction and retention logic
- Increase storage costs unexpectedly

**What to do for historical data**:

- Temporarily increase `reject_old_samples_max_age` during migrations
- Enable `shard_streams.time_sharding_enabled` for backfills
- Plan for several hours of delay before old data is queryable
- Disable time sharding after backfill completes

## Query limits

Query limits protect the query execution path and ensure that compute resources remain available for all users.

### query_timeout

- **Default**: 1 minute
- **Maximum**: 5 minutes (you should not increase beyond this)

The query timeout controls how long a query can execute before being terminated. This limit exists because Loki's query architecture distributes subqueries across worker routines in queriers. Each querier can run a limited number of concurrent workers, and this total compute capacity is shared across all tenants.

**Why you should not exceed 5 minutes**:

A longer timeout allows a single query to reserve worker routines for an extended period. In a multi-tenant environment, if one tenant runs queries with a 10-minute timeout, they could monopolize workers that should be available to other tenants. This creates a negative experience where other users face increased query queue times or timeouts.

**Consequences of exceeding**:

- Reduced query capacity for all tenants
- Increased query queue wait times
- Potential denial of service to other users
- Unpredictable query performance

**What to do instead**: Optimize queries to complete faster by using smaller time ranges, applying filters early, and leveraging query acceleration features like Bloom filters.

### max_label_names_per_series

- **Default**: 15
- **Recommended**: 15 (do not exceed)

This limit controls the maximum number of labels that can be attached to a single log stream. Cardinality is one of the primary cost drivers in Loki's query execution time.

**Why you should not exceed 15 labels**:

Each label added to a stream increases the cardinality of the label set. High cardinality causes Loki to:

- Build massive indexes
- Create excessive numbers of streams
- Fragment data across many small chunks
- Degrade query performance exponentially

Having more than 15 labels per stream typically indicates a fundamental misuse of Loki's label system. Loki is designed for a small set of labels that describe the log source (such as cluster, namespace, job), not for storing metadata as labels.

**Consequences of exceeding**:

- Exponential increase in memory usage on ingesters
- Significantly slower query performance
- Increased storage costs due to index bloat
- Higher CPU usage during queries
- Potential out-of-memory conditions

**What to do instead**:

- Use labels only for low-cardinality source identification
- Parse high-cardinality fields at query time using LogQL parsers
- Store high-cardinality metadata in [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/)

### max_line_size

- **Default**: 256KB
- **Absolute maximum**: 300KB (requests above this are refused)

This limit controls the maximum size in bytes that a single log line can be during ingestion.

**Why you should not exceed 256KB**:

Large log lines have cascading negative effects throughout Loki:

- **Memory pressure**: Ingesters must buffer log lines in memory before compression
- **Compression overhead**: Larger lines take longer to compress and decompress
- **Query performance**: Scanning large chunks slows down query execution
- **Network I/O**: Transferring large lines between components increases latency
- **OOM risk**: Under high load, large lines can cause out-of-memory conditions

While the system may technically accept lines up to 300KB, this impacts performance and stability significantly. Beyond 300KB, the risk of system destabilization is too high.

**Consequences of exceeding**:

- Ingester memory exhaustion
- Slower compression and decompression
- Degraded query performance
- Increased risk of OOM failures
- Potential data loss during high load
- SLA violations

**What to do instead**:

- Structure applications to produce reasonably-sized log lines
- Avoid embedding large payloads (base64-encoded data, full HTTP bodies) in logs
- Log references to external data instead of the data itself
- Split multi-line events into separate log entries

## Understanding limit violations

When limits are exceeded, Loki provides metrics and errors to help you understand what happened:

### Monitoring metrics

- **`loki_discarded_samples_total`**: Counts log lines discarded, with `reason` label indicating which limit was exceeded
- **`loki_discarded_bytes_total`**: Measures bytes discarded by reason
- **`loki_ingester_streams`**: Current number of active streams per tenant

### Common error reasons

- **`rate_limited`**: Exceeded `ingestion_rate_mb`
- **`per_stream_rate_limit`**: Individual stream exceeded its rate limit
- **`stream_limit`**: Exceeded `max_global_streams_per_user`
- **`line_too_long`**: Log line exceeded `max_line_size`
- **`max_label_names_per_series`**: Too many labels on a stream
- **`too_far_behind`**: Log outside the out-of-order window
- **`greater_than_max_sample_age`**: Log older than `reject_old_samples_max_age`

## Best practices for limits

To work effectively within Loki's limits:

1. **Design for low cardinality**: Use 5-10 labels maximum, with static values
2. **Keep logs reasonably sized**: Target 1-10KB per log line
3. **Forward logs promptly**: Aim for sub-second delivery latency
4. **Monitor your usage**: Set up alerts on discarded samples metrics
5. **Scale infrastructure first**: Increase resources before increasing limits
6. **Use alternatives**: Leverage structured metadata and query-time parsing
7. **Plan capacity**: Calculate expected streams before deployment
8. **Test at scale**: Validate label designs with production-like data volumes

## Related documentation

- [Configuration limits best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/bp-limits/) - Practical guide to configuring limits
- [Rate limits and validation](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/request-validation-rate-limits/) - Detailed error handling
- [Understanding cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/) - How cardinality affects performance
- [Labels best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/bp-labels/) - Guide to label design
- [Structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) - Alternative to high-cardinality labels
