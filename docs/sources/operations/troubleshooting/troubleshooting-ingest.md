---
title: Troubleshoot log ingestion (WRITE)
menuTitle: Troubleshoot ingestion
description: Describes how to troubleshoot and debug specific errors when ingesting logs into Grafana Loki.
weight: 
---
<!-- MASTER FILE. -->

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

### Error: `per_stream_rate_limit`

**Error message:**

`Per stream rate limit exceeded (limit: <limit>/sec) while attempting to ingest for stream <stream_labels> totaling <bytes>, consider splitting a stream via additional labels or contact your Loki administrator to see if the limit can be increased`

**Cause:**

A single stream (unique combination of labels) is sending data faster than the per-stream rate limit. This protects ingesters from being overwhelmed by a single high-volume stream.

**Default configuration:**

- `per_stream_rate_limit`: 3 MB/sec
- `per_stream_rate_limit_burst`: 15 MB (5x the rate limit)

**Resolution:**

* **Split the stream** by adding more labels to distribute the load:

   ```yaml
   # Before: {job="app"}
   # After: {job="app", instance="host1"}
   ```

* **Increase per-stream limits** (with caution):

   ```yaml
   limits_config:
     per_stream_rate_limit: 5MB
     per_stream_rate_limit_burst: 20MB
   ```

   {{< admonition type="warning" >}}
   Do not set `per_stream_rate_limit` higher than 5MB or `per_stream_rate_limit_burst` higher than 20MB without careful consideration.
   {{< /admonition >}}

* **Use Alloy's rate limiting** to throttle specific streams:

   ```alloy
   stage.limit {
     rate  = 100
     burst = 200
   }
   ```

**Properties:**

- Enforced by: Ingester
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

* **Adjust chunk idle period** to expire streams faster:

   ```yaml
   ingester:
     chunk_idle_period: 15m  # Default: 30m
   ```

   {{< admonition type="warning" >}}
   Shorter idle periods create more chunks and may impact query performance.
   {{< /admonition >}}

**Properties:**

- Enforced by: Ingester
- Retryable: Yes
- HTTP status: 429 Too Many Requests
- Configurable per tenant: Yes

## Validation errors

Validation errors occur when log data doesn't meet Loki's requirements. These return HTTP status code `400 Bad Request` and are **not retryable**.

### Error: `line_too_long`

**Error message:**

`max entry size <max_size> bytes exceeded for stream <stream_labels> while adding an entry with length <entry_size> bytes`

**Cause:**

A log line exceeds the maximum allowed size.

**Default configuration:**

- `max_line_size`: 256 KB
- `max_line_size_truncate`: false

**Resolution:**

* **Truncate long lines** instead of discarding them:

   ```yaml
   limits_config:
     max_line_size: 256KB
     max_line_size_truncate: true
   ```

* **Increase the line size limit** (not recommended above 256KB):

   ```yaml
   limits_config:
     max_line_size: 512KB
   ```

   {{< admonition type="warning" >}}
   Large line sizes impact performance and memory usage.
   {{< /admonition >}}

* **Filter or truncate logs before sending** using Alloy processing stages:

   ```alloy
   stage.replace {
     expression = "^(.{10000}).*$"
     replace    = "$1"
   }
   ```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `invalid_labels`

**Error message:**

`error parsing labels <labels> with error: <parse_error>`

**Cause:**

Label names or values contain invalid characters or don't follow Prometheus naming conventions:

- Label names must match regex: `[a-zA-Z_][a-zA-Z0-9_]*`
- Label names cannot start with `__` (reserved for Grafana internal use)
- Labels must be valid UTF-8
- Properly escape special characters in label values
- Ensure label syntax follows PromQL format: `{key="value"}`

**Default configuration:**

This validation is always enabled and cannot be disabled.

**Resolution:**

* **Fix label names** in your log shipping configuration:

   ```yaml
   # Incorrect
   labels:
     "123-app": "value"      # Starts with number
     "app-name": "value"     # Contains hyphen
     "__internal": "value"   # Reserved prefix

   # Correct
   labels:
     app_123: "value"
     app_name: "value"
     internal_label: "value"
   ```

* **Use Alloy's relabeling stages** to fix labels:

   ```alloy
   stage.label_drop {
     values = ["invalid_label"]
   }
   
   stage.labels {
     values = {
       app_name = "",
     }
   }
   ```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: `missing_labels`

**Error message:**

`error at least one label pair is required per stream`

**Cause:**

A log stream was submitted without any labels. Loki requires at least one label pair for each log stream.

**Default configuration:**

This validation is always enabled and cannot be disabled. Every stream must have at least one label.

**Resolution:**

Ensure all log streams have at least one label. Update your log shipping configuration to include labels:

```alloy
loki.write "default" {
  endpoint {
    url = "http://loki:3100/loki/api/v1/push"
  }
}

loki.source.file "logs" {
  targets = [
    {
      __path__ = "/var/log/*.log",
      job      = "myapp",
      environment = "production",
    },
  ]
  forward_to = [loki.write.default.receiver]
}
```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: `out_of_order` / `too_far_behind`

These errors occur when log entries arrive in the wrong chronological order.

**Error messages:**

- When `unordered_writes: false`: `entry out of order`
- When `unordered_writes: true`: `entry too far behind, entry timestamp is: <timestamp>, oldest acceptable timestamp is: <cutoff>`

**Cause:**

Logs are being ingested with timestamps that violate Loki's ordering constraints:

- With `unordered_writes: false`: Any log older than the most recent log in the stream is rejected
- With `unordered_writes: true` (default): Logs older than half of `max_chunk_age` from the newest entry are rejected

**Default configuration:**

- `unordered_writes`: true (since Loki 2.4)
- `max_chunk_age`: 2 hours
- Acceptable timestamp window: 1 hour behind the newest entry (half of `max_chunk_age`)

**Resolution:**

* **Ensure timestamps are assigned correctly**:

  - Use a timestamp parsing stage in Alloy if logs have embedded timestamps
  - Avoid letting Alloy assign timestamps at read time for delayed log delivery

   ```alloy
   stage.timestamp {
     source = "timestamp_field"
     format = "RFC3339"
   }
   ```

* **Increase max_chunk_age** (global setting, affects all tenants):

   ```yaml
   ingester:
     max_chunk_age: 4h  # Default: 2h
   ```

   {{< admonition type="warning" >}}
   Larger chunk age increases memory usage and may delay chunk flushing.
   {{< /admonition >}}

* **Split high-volume streams** to reduce out-of-order conflicts:

   ```alloy
   // Add instance or host labels to distribute logs
   loki.source.file "logs" {
     targets = [
       {
         __path__  = "/var/log/*.log",
         job       = "app",
         instance  = env("HOSTNAME"),
       },
     ]
     forward_to = [loki.write.default.receiver]
   }
   ```

* **Check for clock skew** between log sources and Loki. Ensure clocks are synchronized across your infrastructure.

**Properties:**

- Enforced by: Ingester
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No (for `max_chunk_age`)

### Error: `greater_than_max_sample_age`

**Error message:**

`entry for stream <stream_labels> has timestamp too old: <timestamp>, oldest acceptable timestamp is: <cutoff>`

**Cause:**

The log entry's timestamp is older than the configured maximum sample age. This prevents ingestion of very old logs.

**Default configuration:**

- `reject_old_samples`: true
- `reject_old_samples_max_age`: 168 hours (7 days)

**Resolution:**

* **Increase the maximum sample age**:

   ```yaml
   limits_config:
     reject_old_samples_max_age: 336h  # 14 days
   ```

* **Fix log delivery delays** causing old timestamps.

* **For historical log imports**, temporarily disable the check per tenant.

* **Check for clock skew** between log sources and Loki. Ensure clocks are synchronized across your infrastructure.

* **Disable old sample rejection** (not recommended):

   ```yaml
   limits_config:
     reject_old_samples: false
   ```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `too_far_in_future`

**Error message:**

`entry for stream <stream_labels> has timestamp too new: <timestamp>`

**Cause:**

The log entry's timestamp is further in the future than the configured grace period allows.

**Default configuration:**

- `creation_grace_period`: 10 minutes

**Resolution:**

* **Increase the grace period**:

   ```yaml
   limits_config:
     creation_grace_period: 15m
   ```

* **Check for clock skew** between log sources and Loki. Ensure clocks are synchronized across your infrastructure.

* **Verify application timestamps** Validate timestamp generation in your applications.

* **Verify timestamp parsing** in your log shipping configuration.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `max_label_names_per_series`

**Error message:**

`entry for stream <stream_labels> has <count> label names; limit <limit>`

**Cause:**

The stream has more labels than allowed.

**Default configuration:**

- `max_label_names_per_series`: 15

**Resolution:**

* **Reduce the number of labels** by:
  - Using structured metadata for high-cardinality data
  - Removing unnecessary labels
  - Combining related labels

{{< admonition type="warning" >}}
Do not increase `max_label_names_per_series` as high cardinality can lead to significant performance degradation.
{{< /admonition >}}

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `label_name_too_long`

**Error message:**

`stream <stream_labels> has label name too long: <label_name>`

**Cause:**

A label name exceeds the maximum allowed length.

**Default configuration:**

- `max_label_name_length`: 1024 bytes

**Resolution:**

* **Shorten label names** in your log shipping configuration to under the configured limit. You can use abbreviations or shorter descriptive names.

* **Increase the limit** (not recommended):

   ```yaml
   limits_config:
     max_label_name_length: 2048
   ```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `label_value_too_long`

**Error message:**

`stream <stream_labels> has label value too long: <label_value>`

**Cause:**

A label value exceeds the maximum allowed length.

**Default configuration:**

- `max_label_value_length`: 2048 bytes

**Resolution:**

* **Shorten label values** in your log shipping configuration to under the configured limit. You can use hash values for very long identifiers.

* **Use structured metadata** for long values instead of labels.

* **Increase the limit**:

   ```yaml
   limits_config:
     max_label_value_length: 4096
   ```

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: `duplicate_label_names`

**Error message:**

`stream <stream_labels> has duplicate label name: <label_name>`

**Cause:**

The stream has two or more labels with identical names.

**Default configuration:**

This validation is always enabled and cannot be disabled. Duplicate label names are never allowed.

**Resolution:**

* **Remove duplicates** Remove duplicate label definitions in your log shipping configuration to ensure unique label names per stream.

* **Check clients** Check your ingestion pipeline for label conflicts.

* **Verify processing** Verify your label processing and transformation rules.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: `request_body_too_large`

**Error message:**

`Request body too large: <size> bytes, limit: <limit> bytes`

**Cause:**

The HTTP compressed push request body exceeds the configured limit in your gateway/reverse proxy or service that fronts Loki after decompression.

**Default configuration:**

- Loki server max request body size: Configured at the HTTP server level
- Nginx gateway (Helm): `gateway.nginxConfig.clientMaxBodySize` (default: 4 MB)
- Alloy default batch size: 1 MB

**Resolution:**

* **Reduce batch size** in your ingestion client:

   ```alloy
   loki.write "default" {
     endpoint {
       url = "http://loki:3100/loki/api/v1/push"
       
       batch_size  = 524288  // 512KB in bytes
       batch_wait  = "1s"
     }
   }
   ```

* **Split large batches** into smaller, more frequent requests.

* **Increase batch limit** Increase the allowed body size on your gateway/reverse proxy.  For example, in the Helm chart set `gateway.nginxConfig.clientMaxBodySize`; default is 4M.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 413 Request Entity Too Large
- Configurable per tenant: No

## Storage errors

Storage errors occur when Loki cannot write data to its storage backend or the Write-Ahead Log (WAL).

### Error: Disk space full

**Error message:**

`no space left on device`

**Cause:**

Local storage has run out of space. This can affect ingester WAL, chunk cache, or temporary files.

**Default configuration:**

Disk space is not limited by Loki configuration; it depends on your infrastructure provisioning.

**Resolution:**

* **Free up disk space** on Loki instances.
* **Configure retention policies** to remove old data.
* **Scale storage capacity** by adding more disk space.
* **Monitor disk usage** and set up alerts before reaching critical levels.
* **Review compactor settings** to ensure old data is being cleaned up.

**Properties:**

- Enforced by: Operating system
- Retryable: Yes (after freeing space)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: Chunk write failure

**Error message:**

`failed to store chunks: storage unavailable`

**Common causes and errors:**

* **S3/Object Storage errors:**
  - `NoSuchBucket`: Storage bucket doesn't exist
  - `AccessDenied`: Invalid credentials or permissions
  - `RequestTimeout`: Network or storage latency issues

* **DynamoDB errors:**
  - `ProvisionedThroughputExceededException`: Write capacity exceeded
  - `ValidationException`: Invalid data format

* **Cassandra errors:**
  - `NoConnectionsAvailable`: Cannot connect to Cassandra cluster

**Resolution:**

* **Verify storage configuration**:

   ```yaml
   storage_config:
     aws:
       s3: s3://region/bucket-name
       s3forcepathstyle: true
   ```

* **Check credentials and permissions**:
  - Ensure service account has write permissions
  - Verify IAM roles for S3
  - Check API keys for cloud storage

* **Monitor storage health**:
  - Check object storage availability
  - Monitor DynamoDB write capacity
  - Verify Cassandra cluster health

* **Review storage metrics**:
  - `loki_ingester_chunks_flushed_total`
  - `loki_ingester_chunks_flush_errors_total`

* **Increase retries and timeouts**:

   ```yaml
   storage_config:
     aws:
       s3:
         http_config:
           idle_conn_timeout: 90s
           response_header_timeout: 0s
   ```

**Properties:**

- Enforced by: Ingester (during chunk flush)
- Retryable: Yes (with backoff)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: WAL disk full

**Error logged:**

`Error writing to WAL, disk full, no further messages will be logged for this error`

**Metric:** `loki_ingester_wal_disk_full_failures_total`

**Cause:**

The disk where the WAL is stored has run out of space. When this occurs, Loki continues accepting writes but doesn't log them to the WAL, losing durability guarantees.

**Default configuration:**

- WAL enabled by default
- WAL location: configured by `-ingester.wal-dir`
- Checkpoint interval: `ingester.checkpoint-duration` (default: 5 minutes)

**Resolution:**

* **Increase disk space** for the WAL directory.

* **Monitor disk usage** and set up alerts:

   ```promql
   loki_ingester_wal_disk_full_failures_total > 0
   ```

* **Reduce log volume** to decrease WAL growth.

* **Check WAL checkpoint frequency**:

   ```yaml
   ingester:
     checkpoint_duration: 5m
   ```

* **Verify WAL cleanup** is working - old segments should be deleted after checkpointing.

**Properties:**

- Enforced by: Ingester
- Retryable: N/A (writes are accepted but not persisted to WAL)
- HTTP status: N/A (writes succeed, but durability is compromised)
- Configurable per tenant: No

{{< admonition type="note" >}}
The WAL sacrifices durability for availability - it won't reject writes when the disk is full. After disk space is restored, durability guarantees resume. Use metric `loki_ingester_wal_disk_usage_percent` to monitor disk usage.
{{< /admonition >}}

### Error: WAL corruption

**Error message:**

`encountered WAL read error, attempting repair`

**Metric:** `loki_ingester_wal_corruptions_total`

**Cause:**

The WAL has become corrupted, possibly due to:

- Disk errors
- Process crashes during write
- Filesystem issues
- Partial deletion of WAL files

**Default configuration:**

- WAL enabled by default
- Automatic repair attempted on startup
- WAL location: configured by `-ingester.wal-dir`

**Resolution:**

* **Monitor for corruption**:

   ```promql
   increase(loki_ingester_wal_corruptions_total[5m]) > 0
   ```

* **Automatic recovery**: Loki attempts to recover readable data and continues starting.

* **Manual recovery** (if automatic recovery fails):
  - Stop the ingester
  - Backup the WAL directory
  - Delete corrupted WAL files
  - Restart the ingester (may lose some unrecoverable data)

* **Investigate root cause**:
  - Check disk health
  - Review system logs for I/O errors
  - Verify filesystem integrity

**Properties:**

- Enforced by: Ingester (on startup)
- Retryable: N/A (Loki attempts automatic recovery)
- HTTP status: N/A (occurs during startup, not request handling)
- Configurable per tenant: No

{{< admonition type="note" >}}
Loki prioritizes availability over complete data recovery. The replication factor provides redundancy if one ingester loses WAL data. Recovered data may be incomplete if corruption is severe.
{{< /admonition >}}

## Network and connectivity errors

These errors occur due to network issues or service unavailability.

### Error: Connection refused

**Error message:**

`connection refused when connecting to loki:3100`

**Cause:**

The Loki service is unavailable or not listening on the expected port.

**Default configuration:**

- Loki HTTP listen port: 3100 (`-server.http-listen-port`)
- Loki gRPC listen port: 9095 (`-server.grpc-listen-port`)

**Resolution:**

* **Verify Loki is running** and healthy.
* **Check network connectivity** between client and Loki.
* **Confirm the correct hostname and port** configuration.
* **Review firewall and security group** settings.

**Properties:**

- Enforced by: Network/OS
- Retryable: Yes (after service recovery)
- HTTP status: N/A (connection-level error)
- Configurable per tenant: No

### Error: Context deadline exceeded

**Error message:**

`context deadline exceeded`

**Cause:**

Requests are timing out due to slow response times or network issues.

**Default configuration:**

- Alloy default timeout: 10 seconds
- Loki server timeout: `-server.http-server-write-timeout` (default: 30s)

**Resolution:**

* **Increase timeout values** in your ingestion client.
* **Check Loki performance** and resource utilization.
* **Review network latency** between client and server.
* **Scale Loki resources** if needed.

**Properties:**

- Enforced by: Client/Server
- Retryable: Yes
- HTTP status: 504 Gateway Timeout (or client-side timeout)
- Configurable per tenant: No

### Error: Service unavailable

**Error message:**

`service unavailable (503)`

**Cause:**

Loki is temporarily unable to handle requests due to high load or maintenance. This can occur when ingesters are unhealthy or the ring is not ready.

**Default configuration:**

- Ingester ring readiness: Requires minimum healthy ingesters based on replication factor
- Distributor: Returns 503 when no healthy ingesters available

**Resolution:**

* **Implement retry logic** with exponential backoff in your client.
* **Scale Loki ingester instances** to handle load.
* **Check for resource constraints** (CPU, memory, storage).
* **Review ingestion patterns** for sudden spikes.

**Properties:**

- Enforced by: Distributor/Gateway
- Retryable: Yes
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

## Structured metadata errors

These errors occur when using structured metadata incorrectly.

### Error: Structured metadata disabled

**Error message:**

`stream <stream_labels> includes structured metadata, but this feature is disallowed. Please see limits_config.allow_structured_metadata or contact your Loki administrator to enable it`

**Cause:**

Structured metadata is disabled in the Loki configuration. This feature must be explicitly enabled.

**Default configuration:**

- `allow_structured_metadata`: true (enabled by default in recent versions)

**Resolution:**

* **Enable structured metadata**:

   ```yaml
   limits_config:
     allow_structured_metadata: true
   ```

* **Move metadata to regular labels** if structured metadata isn't needed.
* **Contact your Loki administrator** to enable the feature.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Structured metadata too large

**Error message:**

`stream '{job="app"}' has structured metadata too large: '70000' bytes, limit: '65536' bytes. Please see limits_config.max_structured_metadata_size or contact your Loki administrator to increase it`

**Cause:**

The structured metadata size exceeds the configured limit.

**Default configuration:**

- `max_structured_metadata_size`: 64 KB

**Resolution:**

* **Reduce the size** of structured metadata by removing unnecessary fields.

* **Increase the limit**:

   ```yaml
   limits_config:
     max_structured_metadata_size: 128KB
   ```

* **Use compression or abbreviations** for metadata values.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Too many structured metadata entries

**Error message:**

`stream '{job="app"}' has too many structured metadata labels: '150', limit: '128'. Please see limits_config.max_structured_metadata_entries_count or contact your Loki administrator to increase it`

**Cause:**

The number of structured metadata entries exceeds the limit.

**Default configuration:**

- `max_structured_metadata_entries_count`: 128

**Resolution:**

* **Reduce the number** of structured metadata entries by consolidating fields.

* **Increase the limit**:

   ```yaml
   limits_config:
     max_structured_metadata_entries_count: 256
   ```

* **Combine related metadata** into single entries using JSON or other formats.

**Properties:**

- Enforced by: Distributor
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

## Blocked ingestion errors

These errors occur when ingestion is administratively blocked.

### Ingestion blocked

**Error message:**

`ingestion blocked for user <tenant> until <time> with status code 260`

Or for policy-specific blocks:

`ingestion blocked for user <tenant> (policy: <policy>) until <time> with status code 260`

**Cause:**

Ingestion has been administratively blocked for the tenant, typically due to policy violations, billing issues, or maintenance. Status code 260 is a custom Loki status code for blocked ingestion.

**Default configuration:**

- `blocked_ingestion_status_code`: 260 (custom status code)
- Blocks are configured per tenant or per policy in runtime overrides

**Resolution:**

* **Wait until the block expires** if a time limit is set.
* **Contact your Loki administrator** to understand the block reason.
* **Review tenant usage patterns** and policies.
* **Check for policy-specific blocks** if using ingestion policies.

**Properties:**

- Enforced by: Distributor
- Retryable: No (until block is lifted)
- HTTP status: 260 (custom) or configured status code
- Configurable per tenant: Yes

## Troubleshooting workflow

Follow this workflow when investigating ingestion issues:

* **Check metrics**:

   ```promql
   # See which errors are occurring
   sum by (reason) (rate(loki_discarded_samples_total[5m]))
   
   # Identify affected tenants
   sum by (tenant, reason) (rate(loki_discarded_bytes_total[5m]))
   ```

* **Review logs** from distributors and ingesters:

   ```bash
   # Enable write failure logging per tenant
   limits_config:
     limited_log_push_errors: true
   ```

* **Test push endpoint**:

   ```bash
   curl -H "Content-Type: application/json" \
        -XPOST http://loki:3100/loki/api/v1/push \
        --data-raw '{"streams":[{"stream":{"job":"test"},"values":[["'"$(date +%s)000000000"'","test log line"]]}]}'
   ```

* **Check tenant limits**:

   ```bash
   curl http://loki:3100/loki/api/v1/user_stats
   ```

* **Review configuration** for affected tenant or global limits.

* **Monitor system resources**:
  - Ingester memory usage
  - Disk space for WAL
  - Storage backend health
  - Network connectivity

## Additional resources

- [Loki Configuration Reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/)
- [Request Validation and Rate Limits](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/request-validation-rate-limits/)
- [Write-Ahead Log Documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/wal/)
- [Alloy Configuration](https://grafana.com/docs/alloy/<ALLOY_VERSION>/reference/components/loki/)
- [Alloy loki.source.file](https://grafana.com/docs/alloy/<ALLOY_VERSION>/reference/components/loki.source.file/)
- [Alloy loki.write](https://grafana.com/docs/alloy/<ALLOY_VERSION>/reference/components/loki.write/)
- [Multi-tenancy and Limits](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/)
