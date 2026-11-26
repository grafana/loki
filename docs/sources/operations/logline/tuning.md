---
title: Tune the Full-Text Index
menuTitle: Tuning
description: Tune the Full-Text Index query acceleration by identifying high consumption lag and adjusting extraction and merge parallelism.
weight: 20
---

# Tune the Full-Text Index

{{< admonition type="warning" >}}
The Full-Text Index is an [experimental feature](https://grafana.com/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided. The Full-Text Index requires [Kafka-based ingestion]({{< relref "../kafka.md" >}}). Configuration options, index formats, and behavior can change. Validate index coverage and query results before enabling live acceleration.
{{< /admonition >}}

This page explains how to investigate high consumption lag and recover indexing throughput when using the Full-Text Index for query acceleration. For the architecture and query examples, refer to [Full-Text Index](../). For prerequisites and deployment steps, refer to [Configure the Full-Text Index]({{< relref "./configure.md" >}}).

## High consumption lag

The Full-Text Index builders consume log records from Kafka and turn them into indexes. Consumption lag grows when they cannot keep up with incoming records. Common causes include CPU limits, slow local merges, insufficient scratch disk space, failed uploads, and repeated restarts.

High lag is not just a performance problem. In live mode, it can also affect which logs queries return.

If index coverage is uncertain, protect query results before tuning. Identify the bottleneck, then apply only the extraction or merge changes that address it; you don't need to change both settings. Verify recovery before restoring live queries.

### How lag can cause missing query results

Loki normally bypasses index narrowing for query splits whose read end falls within `querier.query_ingesters_within`, which defaults to 3 hours. Older splits can use the index. This protection is based on time; it does not automatically expand when builders fall behind.

{{< admonition type="warning" >}}
If builder consumption lag exceeds `querier.query_ingesters_within`, matching logs can age out of the protected recent-data window before their indexes are available. Live queries can then omit logs that are still present in Loki. Missing or incomplete indexes do not necessarily trigger a fallback to a full scan.
{{< /admonition >}}

For example, with a 3-hour recent-data window and a 4-hour indexing delay, an eligible live query for logs from 3.5 hours ago can return incomplete results. This does not necessarily mean the logs have been lost: the query can find them through the normal path when index narrowing is disabled.

The index availability delay is the total time from a record being written to Kafka until query components can use its index and current hints. Consumption lag measures only part of this delay. When setting a positive value for `querier.query_ingesters_within`, allow for all of the following:

- Time waiting to consume Kafka records.
- Time accumulating data before a flush.
- Time finalizing and uploading index files, including retries.
- Time until query components discover those files.
- Staleness of cached hints.

Keep the total index availability delay comfortably below `querier.query_ingesters_within`, the protected recent-data window. A consumption lag metric below the window is not, by itself, evidence that index coverage is complete. Late-arriving and replayed logs also need separate validation because log timestamps and Kafka record timestamps can differ.

### Protect query results while recovering

Disable live narrowing while coverage is uncertain. For an individual request, send `X-Logline-Index: off`.

For a deployment-wide bypass, disable the query integration on query frontends and queriers:

```yaml
logline:
  query:
    enabled: false
```

The equivalent flag is `-logline-query.enabled=false`. Keep builders running so they can catch up. Do not rely only on tenant mode `off` or the process dry-run default: a recognized `X-Logline-Index: live` header overrides those modes while the query integration is enabled.

Do not set `querier.query_ingesters_within: 0` as a workaround. The Full-Text Index needs a positive value to protect recent query splits. Increasing the window also changes ordinary ingester querying and is not a substitute for restoring complete index coverage.

### Identify the bottleneck

Inspect lag by partition rather than averaging across the builder pool. For example, scope this query to your deployment:

```promql
max by (partition) (logline_index_builder_consumption_lag_seconds)
```

The metric estimates how far a partition is behind Kafka using record timestamps. It can report zero when consumption has caught up or no records arrive in a fetch. Zero lag does not prove that the records have been indexed, uploaded, and discovered.

Check the following signals before changing parallelism:

| Signal | What to investigate |
| --- | --- |
| Growing `logline_index_builder_consumption_lag_seconds` with CPU saturation or throttling | Extraction throughput and the builder's CPU allocation. |
| Long `logline_index_builder_merge_duration_seconds` compared with total flush duration | Local index merging may be limiting progress. |
| Increasing `logline_index_builder_flush_backpressure_seconds_total` | The consumer is waiting for an earlier flush to finish. Check merge and upload performance. |
| Increasing `logline_index_builder_flush_errors_total` | Use the `step` label and builder logs to distinguish preparation, upload, and offset-commit failures. |
| Increasing `logline_index_builder_decode_errors_total` or repeated restarts | Resolve the failing records or configuration before adding concurrency. |
| Low scratch space, high disk latency, or memory pressure | Restore resource headroom. More threads can make these bottlenecks worse. |

Compare merge time with `logline_index_builder_flush_duration_seconds` and `logline_index_builder_upload_duration_seconds`. These are histograms; use their histogram series when calculating rates or quantiles. If uploads dominate, increasing extraction or merge threads will not fix object storage access or network failures.

Adding builder replicas can help when there are enough Kafka partitions to distribute. It does not let multiple builders in the same consumer group process one partition concurrently, so check whether lag is concentrated on a single busy partition.

### Increase extraction parallelism when CPU-bound

`logline.builder.extract_threads` controls how many workers extract index terms from incoming logs. It defaults to `1` and accepts values from `1` to `4`. Values above `1` are intended for catch-up, not as a default recommendation for every deployment.

If extraction is the bottleneck, provision CPU and memory headroom, then increase the setting gradually. For example:

```yaml
logline:
  builder:
    extract_threads: 3
```

The equivalent flag is `-logline-builder.extract-threads=3`.

Each extraction worker has its own sort buffers. At the default `postings_buffer_pairs`, those buffers require approximately 460 MiB per worker, and two sets can coexist during a builder swap. With three workers, budget approximately 2.7 GiB for sort buffers alone, plus decoding, metadata, merging, uploads, and other process overhead. Adjust container memory, CPU, and `GOMEMLIMIT` accordingly.

Watch whether lag starts decreasing and whether restarts, memory pressure, or flush backpressure increase. More extraction workers do not help when CPU is already constrained or another stage is limiting progress.

### Increase merge parallelism when flush merging is slow

`logline.builder.merge_threads` controls how many index shards a builder merges concurrently during a flush. It defaults to `1`. This is the builder's local merge stage, not the separately deployed `logline-compactor` service.

Increase it only when merge time is a bottleneck and the existing index configuration has multiple n-gram shards. The value must not exceed `logline.index.shard_count`; an unsharded configuration (`shard_count` of `0` or `1`) supports only one merge thread.

For an existing configuration with at least two shards, an example is:

```yaml
logline:
  builder:
    merge_threads: 2
```

The equivalent flag is `-logline-builder.merge-threads=2`.

Each concurrent shard merge adds memory use, disk I/O, and open file descriptors. Increase parallelism incrementally and verify that total flush time improves. Adding extraction workers can move the bottleneck to merging, so reassess the whole pipeline after each change.

### Verify recovery before restoring live queries

Before restoring live mode:

1. Confirm that lag is falling and remains low across every partition, not just the average.
2. Confirm that pending data has been flushed and uploaded successfully.
3. Allow query components to discover the new indexes and cached hints to expire, and check for discovery errors.
4. Compare representative historical query results with the Full-Text Index disabled and with a limited live trial, including the period affected by the lag.
5. Restore live acceleration gradually and continue monitoring.

After catch-up, return temporary `extract_threads` and `merge_threads` changes to their normal values unless ongoing measurements justify keeping them. For scratch disk sizing and other capacity settings, refer to [Sizing and tuning]({{< relref "./configure.md#sizing-and-tuning" >}}).

## Troubleshoot errors

For specific error messages from builders and query components, refer to [Troubleshoot the Full-Text Index]({{< relref "../troubleshooting/troubleshoot-logline.md" >}}).
