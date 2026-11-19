---
title: Configure Logline
menuTitle: Configure
description: Prerequisites and configuration for enabling, tuning, and monitoring Logline query acceleration in Grafana Loki.
weight: 10
---

# Configure Logline

This page covers the prerequisites, deployment steps, tuning, retention, and monitoring for Logline. For an introduction to its architecture and query behavior, refer to [Logline](../).

## Prerequisites and index coverage

Before enabling Logline, you need:

- A Kafka log topic populated by Loki's distributor. Refer to [Kafka-based ingestion]({{< relref "../kafka.md" >}}) for write-path configuration. Logline builders must be able to read every partition containing the logs to be indexed.
- Access to the ingester partition ring through the same `ingester.kafka_ingestion.partition_ring.kvstore` configuration and KV prefix as the ingesters. With memberlist, configure `memberlist.join_members` so builders join the existing cluster. Builders fail startup if the ring remains empty.
- Object storage configured through `storage_config.object_store`, or `common.storage.object_store`, and the object store selected by the current schema period. Legacy settings such as `storage_config.aws` or `storage_config.gcs` alone are not sufficient. All Logline components must resolve the same bucket and prefix, `logline/` by default.
- Local scratch storage for index builders, durable job-state storage for the Logline scheduler, and read/write permissions appropriate to each component. The scheduler also needs permission to delete indexes.
- A deliberate first trusted index date, configured with `-logline-store.min-date`.

### Choose a trusted start date

`-logline-store.min-date` is required and uses the format `YYYY-MM-DD`, interpreted as midnight UTC. Builders discard log entries before this date, and queries before it run without Logline narrowing. The current builder requires a date on or after January 1, 2026.

Choose a date from which you can establish complete index coverage. Starting a builder does not backfill logs from object storage. Historical coverage is limited by records available in Kafka and the builder consumer group's offsets. Do not move the trusted date backward without first establishing coverage for the additional period.

{{< admonition type="warning" >}}
Missing historical indexes are not equivalent to an index lookup error. A successful lookup that finds no indexes or matching documents can produce no hints, causing live queries to skip data. Configure the same trusted date, bucket, and prefix across components, and verify complete coverage before using live mode. An incorrect storage location, missing Kafka records, or prematurely deleted indexes can produce incomplete query results.
{{< /admonition >}}

Recent query splits within `querier.query_ingesters_within` run without narrowing. This setting defaults to 3 hours. Separately, index discovery excludes indexes whose earliest Kafka record timestamp falls inside that window. Late-arriving and replayed logs therefore need particular care: log timestamps and Kafka record timestamps are not interchangeable coverage boundaries.

Keep a positive recent-data window and account for builder lag, flushing, index discovery, and hint-cache delays before enabling live queries. A recent-data window is not a substitute for verifying index coverage.

## Enable Logline

Configure Logline through the top-level `logline:` YAML block or equivalent command-line flags. Explicit command-line flags take precedence over YAML settings. The tenant settings described below are configured through `limits_config` or runtime overrides.

The following examples show Logline configuration and additional process arguments, not a complete Loki deployment. First verify the storage and ring prerequisites above, and retain your Kafka, networking, and authentication configuration. Logline opens the bucket selected by the schema period covering the current time; it does not search all historical schema-period buckets. Plan index continuity before changing that bucket.

### 1. Configure shared index settings

Use consistent settings across builders, readers, and compaction components. For example:

```yaml
logline:
  store:
    min_date: "2026-11-01"
    bucket_prefix: logline/
  index:
    version: v4
    ngram_length: 6
```

The equivalent command-line flags are:

```text
-logline-store.min-date=2026-11-01
-logline-store.bucket-prefix=logline/
-logline-index.version=v4
-logline-index.ngram-length=6
```

Replace the example date with your verified coverage boundary. The n-gram length is not recorded in the index: every component must use the same value. Do not change it as an ordinary rolling configuration update against existing indexes.

The default index format is `v4`.

### 2. Deploy index builders

Run the `logline-index-builder` target with writable scratch storage:

```text
-target=logline-index-builder
-logline-builder.scratch-dir=/var/loki/logline-scratch
```

Builders inherit unset Kafka connection settings from `kafka_config`. You can override them with `-logline-builder.kafka.*` flags. Builders use their own consumer group, `logline-index-builder` by default. Do not share this group with ingesters or unrelated consumers.

Each builder uses a static membership ID, defaulting to its hostname. Give each replica a unique, stable identity. The default Kafka session timeout is 2 minutes; the broker's `group.max.session.timeout.ms` must allow that value.

Builders upload indexes before committing Kafka offsets. After a restart or rebalance, records can be replayed and produce overlapping indexes. Compaction merges those indexes. Scale the builder pool through Kafka consumer-group partition assignment, and monitor lag rather than assuming that more replicas always increase throughput.

### 3. Deploy the scheduler and compactors

The `logline-scheduler` target plans compaction and deletes expired or superseded indexes. Run exactly one active scheduler per index store, including during upgrades: stop the old instance before starting its replacement. There is no leader election between schedulers. Give it a durable directory for in-flight job state:

```text
-target=logline-scheduler
-logline-compaction.job-state-dir=/var/loki/logline-jobs
```

Run `logline-compactor` workers with the scheduler's HTTP base URL:

```text
-target=logline-compactor
-logline-compaction.scheduler-address=http://logline-scheduler:3100
```

Use the same index store settings as the builders. Compactors merge compatible indexes for the same date and n-gram shard into fewer, larger indexes. Preserve scheduler job state across restarts: losing it can assign the same source indexes to multiple jobs, and redundant compactions cannot be automatically deduplicated.

### 4. Enable query lookups without changing results

Enable Logline on query frontends and queriers, with the same shared index settings. Start with header opt-in and dry-run as the process default:

```text
-logline-query.enabled=true
-logline-query.dry-run=true
-logline-query.require-opt-in-header=true
```

With tenant mode unset, queries without an opt-in header execute normally. Send `X-Logline-Index: dry_run` on representative query requests to run hint lookups alongside normal query execution and verify returned results against the hints.

By default, queries estimated to scan less than 10 GiB skip index lookups. For a controlled test, you can set the tenant limit `logline_query_min_query_bytes_for_index` to `0` to disable this size threshold.


### 5. Progress from dry-run to live queries

After verifying coverage and results, send `X-Logline-Index: live` for a limited set of log queries. Compare their results, bytes scanned, and latency with requests using `X-Logline-Index: off`.

You can also select a mode through tenant runtime overrides:

```yaml
overrides:
  "tenant-a":
    logline_query_mode: dry_run
    logline_query_min_query_bytes_for_index: 0
```

Change the tenant mode to `live` only after validation, and restore an appropriate minimum query size after testing. Valid modes are `off`, `dry_run`, `live`, and the empty string. An empty mode uses the process defaults and header opt-in policy. An explicit tenant mode activates that mode without requiring an opt-in header.

{{< admonition type="warning" >}}
Recognized `X-Logline-Index` header values override both tenant mode and the process dry-run default. In particular, `live` can override tenant mode `off`. Header opt-in and tenant mode are rollout controls, not access-control boundaries. Control which clients can supply this header during validation.
{{< /admonition >}}

To disable Logline for a request, use `X-Logline-Index: off`. To disable the query integration at the process level, set `-logline-query.enabled=false` on query frontends and queriers. Neither action deletes indexes or stops separately deployed builders and compactors.

## Sizing and tuning

Start with the defaults and measure your ingestion volume, search selectivity, builder lag, and query overhead. There is no fixed throughput or storage-saving guarantee.

| Setting | Default | Operational effect |
| --- | --- | --- |
| `-logline-index.document-interval` | `16s` | Time covered by a document. Smaller intervals can improve time-range selectivity but increase indexing work. |
| `-logline-builder.postings-buffer-pairs` | `20000000` | In-memory term/document pair capacity. Sort buffers cost approximately 24 bytes per pair. |
| `-logline-builder.flush-on-max-bytes` | `21474836480` (20 GiB) | Scratch run-file bytes that trigger a flush; not a cap on peak disk use. |
| `-logline-builder.flush-on-max-age` | `30m` | Maximum accumulation age before a flush is triggered, not the age of log entries. |
| `-logline-builder.flush-on-idle` | `5m` | Inactivity period before a flush is triggered. |
| `-logline-store.poll-interval` | `15m` | Index discovery interval; also controls scheduler planning and deletion passes. |
| `-logline-query.hint-timeout` | `15s` | Maximum wait for hints per live subrequest before that subrequest runs normally. |
| `-logline-query.max-hint-parallel` | `64` | Maximum concurrent index lookup workers per query. |
| `-logline-query.hint-cache-ttl` | `2m` | Hint-cache lifetime. Set to `0` to disable hint caching. |

The default builder sort buffers occupy approximately 460 MiB. During a builder swap, two sets can coexist. Budget additional memory for decoding, document metadata, merging, and uploads; the estimated-memory metric is not a total process memory measurement. Increasing extraction or merge parallelism increases resource requirements.

Size builder scratch storage for approximately two to three times `flush-on-max-bytes`, because active runs, retiring runs, and merged output can coexist. At the default threshold, that means approximately 40–60 GiB before additional operational headroom. Builder startup clears its scratch directory, so use a dedicated path that contains no other data.

New indexes become available only after building, uploading, and discovery. Cached hints can delay their use further. A failed background discovery poll keeps the previous snapshot; it does not automatically disable live narrowing.

## Retention

Logline indexes are shared across tenants. The scheduler uses the longest nonzero `retention_period` from `limits_config` and configured tenant overrides to expire indexes. If no nonzero period is configured, age-based index deletion is disabled. Per-selector `retention_stream` rules are not used to calculate this period.

{{< admonition type="warning" >}}
A tenant's `retention_period: 0` does not prevent index deletion when the default or another tenant has a positive retention period. For example, a default of `0` and one tenant with `7d` produces 7-day index retention, even if other tenants retain their logs indefinitely.
{{< /admonition >}}

Ensure index retention covers every log time range where live narrowing can apply, including logs retained indefinitely or longer by per-selector retention rules. Do not apply an object storage lifecycle policy that removes indexes while their logs remain queryable through live Logline.

After compaction, source indexes remain available for `-logline-retention.compacted-source-delete-delay`, which defaults to 1 hour. This delay lets readers using older discovery snapshots finish reading the source indexes. Account for discovery delays and in-flight reads before reducing it.

## Monitoring

Monitor both index coverage and query effectiveness:

| Signal | What to check |
| --- | --- |
| `logline_index_builder_consumption_lag_seconds` | Growing lag means builders are falling behind Kafka. Low lag alone does not prove indexes have been flushed and discovered. |
| `logline_index_builder_flush_errors_total` | Investigate preparation, upload, or commit failures that prevent progress. |
| `logline_index_builder_run_disk_bytes` | Compare active run bytes with the flush threshold and actual free scratch space. |
| `logline_index_builder_estimated_memory_bytes` | Track the builder's accounted working set alongside process memory. |
| `logline_index_store_poll_errors_total` | Failed discovery can leave query components using stale index snapshots. |
| `logline_query_frontend_dry_run_verified_total{result="false_negative"}` | Investigate any increase before expanding live usage. |
| `logline_query_frontend_hint_wait_seconds` | Measure time live queries spend waiting for hints, not total query overhead. |
| `logline_query_frontend_hint_passthrough_total` | Check why lookups did not narrow queries. |
| `logline_query_frontend_hint_skipped_small_query_total` | Check whether the minimum query-size threshold is excluding test queries. |

## Next steps

For high consumption lag, missing results, or queries that do not accelerate, refer to [Troubleshoot Logline]({{< relref "./troubleshooting.md" >}}).
