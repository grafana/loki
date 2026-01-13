---
title: Troubleshoot log ingestion (WRITE)
menuTitle: Troubleshoot ingestion
description: Describes how to troubleshoot and debug specific errors when ingesting logs into Grafana Loki.
weight: 
---

# Troubleshoot log ingestion (WRITE)

This guide helps you troubleshoot errors that occur when ingesting logs into Loki and writing logs to storage. When Loki rejects log ingestion requests, it's typically due to exceeding rate limits, violating validation rules, or encountering storage issues.

Before you begin, ensure you have the following:

- Access to Grafana Loki logs and metrics
- Understanding of your log ingestion pipeline (Alloy or custom clients)
- Permissions to configure limits and settings if needed

## Monitoring ingestion errors

All ingestion errors are tracked using Prometheus metrics:

- `loki_discarded_samples_total` - Count of discarded log samples by reason
- `loki_discarded_bytes_total` - Volume of discarded log data by reason

The `reason` label on these metrics indicates the specific error type. Set up alerts on these metrics to detect ingestion problems.

## Rate limit errors

Rate limits protect Loki from being overwhelmed by excessive log volume. When rate limits are exceeded, Loki returns HTTP status code `429 Too Many Requests`.

### Error: `rate_limited`

**Error message:**

`ingestion rate limit exceeded for user <tenant> (limit: <limit> bytes/sec) while attempting to ingest <lines> lines totaling <bytes> bytes, reduce log volume or contact your Loki administrator to see if the limit can be increased`

**Cause:**

The tenant has exceeded their configured ingestion rate limit. This is a global per-tenant limit enforced by the distributor.

**Default configuration:**

- `ingestion_rate_mb`: 4 MB/sec
- `ingestion_burst_size_mb`: 6 MB
- `ingestion_rate_strategy`: "global" (shared across all distributors)

**Resolution:**

* **Increase rate limits** (if you have sufficient cluster resources):

   ```yaml
   limits_config:
     ingestion_rate_mb: 8
     ingestion_burst_size_mb: 12
   ```

* **Reduce log volume** by:

  - Filtering unnecessary log lines using Alloy processing stages
  - Collecting logs from fewer targets
  - Sampling high-volume streams
  - Using Alloy's rate limiting and filtering capabilities

* **Switch to local strategy** (if using single-region deployment):

   ```yaml
   limits_config:
     ingestion_rate_strategy: local
   ```

   {{< admonition type="note" >}}
   This multiplies the effective limit by the number of distributor replicas.
   {{< /admonition >}}

**Properties:**

- Enforced by: Distributor
- Retryable: Yes
- HTTP status: 429 Too Many Requests
- Configurable per tenant: Yes

### Error: `stream_limit`

**Error message:**

`maximum active stream limit exceeded when trying to create stream <stream_labels>, reduce the number of active streams (reduce labels or reduce label values), or contact your Loki administrator to see if the limit can be increased, user: <tenant>`

**Cause:**

The tenant has reached the maximum number of active streams. Active streams are held in memory on ingesters, and excessive streams can cause out-of-memory errors.

**Default configuration:**

- `max_global_streams_per_user`: 5000 (globally)
- `max_streams_per_user`: 0 (no local limit per ingester)
- Active stream window: `chunk_idle_period` (default: 30 minutes)

**Terminology:**

- **Active stream**: A stream that has received logs within the `chunk_idle_period` (default: 30 minutes)

**Resolution:**

* **Reduce stream cardinality** by:

  - Using fewer unique label combinations by removing unnecessary labels
  - Avoiding high-cardinality labels (IDs, timestamps, UUIDs)
  - Using structured metadata instead of labels for high-cardinality data

* **Increase stream limits** (if you have sufficient memory):

   ```yaml
   limits_config:
     max_global_streams_per_user: 10000
   ```

{{< admonition type="note" >}}
Do not increase stream limits to accommodate high cardinality labels, this can result in Loki flushing extremely high numbers of small files which will make for extremely poor query performance.  As volume and the size of the infrastructure being monitored increase, it would be expected to increase the stream limit. However even for hundreds of TBs per day of logs you should avoid exceeding 300,000 max global streams per user.
{{< /admonition >}}

**Properties:**

- Enforced by: Ingester
- Retryable: Yes
- HTTP status: 429 Too Many Requests
- Configurable per tenant: Yes
