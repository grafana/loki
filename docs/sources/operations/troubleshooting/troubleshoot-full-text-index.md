---
title: Troubleshoot the Full-Text Index
menuTitle: Troubleshoot the Full-Text Index
description: Diagnose Full-Text Index builder, storage, and query lookup messages that affect indexing and query acceleration.
weight: 110
---

# Troubleshoot the Full-Text Index

{{< admonition type="warning" >}}
The Full-Text Index is an [experimental feature](https://grafana.com/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided. The Full-Text Index requires [Kafka-based ingestion]({{< relref "../kafka.md" >}}). Configuration options, index formats, and behavior can change. Validate index coverage and query results before enabling live acceleration.
{{< /admonition >}}

This page explains specific messages logged by Full-Text Index builders and query components. Before you begin, ensure you can access component logs, inspect Kafka and object storage connectivity, and view the deployment's configuration.

For high consumption lag without a specific error, refer to [Tune the Full-Text Index]({{< relref "../full-text-index/tuning.md" >}}). If index coverage is uncertain, [disable live narrowing]({{< relref "../full-text-index/tuning.md#protect-query-results-while-recovering" >}}) while investigating.

## Builder errors

### Error: Flush failed

**Error message:**

```console
flush failed
```

The log entry includes `step` and `err` fields. A slow or failing flush can also produce:

```console
backpressure: blocking consumption until pending flush completes
```

**Cause:**

The builder cannot finish preparing index files, uploading them, or committing Kafka offsets. A pending flush can block consumption and increase lag.

**Resolution:**

Use the `step` field and the underlying `err` to identify the failing operation:

| Step | What to check |
| --- | --- |
| `prepare_flush_files` | Scratch disk capacity, permissions, I/O errors, and local merge performance. |
| `upload_flush_files` | Object storage connectivity, credentials, and permissions to list objects, check whether an index exists, and upload indexes and metadata. |
| `commit_offsets` | Kafka connectivity, authorization, and consumer-group membership errors. |

The builder retries failed flush steps. Fix the underlying error rather than increasing thread counts without identifying the bottleneck. Transient storage failures can recover on retry. Corrupt or truncated scratch files, or a latched extraction-worker I/O failure, can make `prepare_flush_files` fail repeatedly even after storage access is restored. In those cases, correct the storage problem and restart the builder: startup clears scratch state and the builder reconsumes from committed Kafka offsets. Index uploads must succeed before their Kafka offsets are committed. For slow merges without storage errors, refer to [merge parallelism]({{< relref "../full-text-index/tuning.md#increase-merge-parallelism-when-flush-merging-is-slow" >}}).

**Properties:**

- Enforced by: Full-Text Index builder
- Retryable: Yes, failed flush steps retry automatically
- HTTP status: N/A (component log message)
- Configurable per tenant: No

### Error: Scratch storage failure

**Error messages:**

```console
failed to create scratch directory: <error>
failed to read scratch directory: <error>
run spill failed (partition=<partition> offset=<offset>): <error>
```

Angle-bracketed values represent the actual error and Kafka record details.

**Cause:**

The builder cannot create or read its scratch directory, or cannot write an intermediate index file. The underlying error can identify a full disk, insufficient permissions, or another local I/O failure.

**Resolution:**

- Ensure `logline.builder.scratch_dir` is writable by the builder process and has sufficient free space.
- Use a dedicated scratch directory: builder startup clears its contents.
- Provision the headroom described in [Sizing and tuning]({{< relref "../full-text-index/configure.md#sizing-and-tuning" >}}). Increasing parallelism is not a remedy for a full disk.
- After correcting the storage problem, restart a failed builder so it can resume from committed Kafka offsets. Restarting without fixing the underlying problem can repeat the failure.

**Properties:**

- Enforced by: Full-Text Index builder
- Retryable: Requires correcting the storage problem; failed startup or record processing requires a restart
- HTTP status: N/A (component startup or processing failure)
- Configurable per tenant: No

### Error: Kafka record decoding failed

**Error message:**

```console
failed to decode protobuf stream (partition=<partition> offset=<offset> size=<size>): <error>
```

**Cause:**

The builder cannot decode a record from its configured Kafka topic. The message identifies the partition, offset, record size, and decoding error. The builder fails without advancing the batch's offsets, so it encounters the uncommitted record again after a restart.

**Resolution:**

- Confirm that the builder is consuming the intended Loki log topic.
- Use the partition and offset to investigate the failing record and the upstream producer's format compatibility.
- Correct topic or producer compatibility problems. If a stored record is malformed, work with the producer owner on a recovery or replay plan that preserves index coverage. Restarting alone encounters the same uncommitted record again.
- Do not bypass records by advancing consumer offsets without a plan to restore index coverage: gaps can cause incomplete live query results.

**Properties:**

- Enforced by: Full-Text Index builder
- Retryable: Requires correcting the record or configuration and restarting the failed service
- HTTP status: N/A (component processing failure)
- Configurable per tenant: No

## Index discovery errors

### Error: Index polling failed

**Error messages:**

```console
initial poll failed
poll failed
```

The `err` field can include a message such as:

```console
poll list dates failed: <error>
poll list failed: <error>
failed to get meta.json at <path>: <error>
```

**Cause:**

The index store cannot list index objects or read their metadata. An initial poll failure prevents polling startup. Later failures can leave query components using an older index snapshot.

**Resolution:**

- Inspect the wrapped storage error for connectivity, authentication, authorization, or missing-object problems.
- Confirm that all components use the same bucket and `logline.store.bucket_prefix`.
- Restore object listing and metadata read access. Subsequent polling attempts retry at `logline.store.poll_interval`; restart a component whose polling failed to start after correcting the cause.
- Verify that readers have discovered current indexes before restoring live narrowing.

**Properties:**

- Enforced by: Full-Text Index store
- Retryable: Background polls retry automatically; an initial polling failure requires a successful restart
- HTTP status: N/A (component log message)
- Configurable per tenant: No

## Queries run without acceleration

The following query frontend messages indicate a fallback to normal query execution, not a dedicated HTTP query error:

```console
hint prefetch timeout, falling back to passthrough
hint provider error, passing through
```

For a timeout, investigate index lookup latency. For a provider error, inspect the attached error and index storage access. Also check the query integration switch, header or tenant mode, minimum query size, [supported query patterns]({{< relref "../full-text-index/_index.md#query-examples" >}}), and trusted/recent time boundaries.

Any nonempty `X-Logline-Skip-Cache` header bypasses the hint cache for that request. It does not force index discovery or bypass Loki's normal query results cache. Account for the results cache when comparing accelerated and unaccelerated queries.

A successful lookup against incomplete index coverage is different from a lookup error: it can omit matching logs without triggering fallback. If results are missing, disable live narrowing, investigate [high consumption lag]({{< relref "../full-text-index/tuning.md#high-consumption-lag" >}}), and verify the trusted start date, Kafka coverage, storage location, reader discovery, and retention before re-enabling it.
