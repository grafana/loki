---
title: Enforce rate limits and push request validation
menuTitle: Rate limits
description: Describes the different rate limits and push request validation and their error handling.
weight: 
---
# Enforce rate limits and push request validation

Loki will reject requests if they exceed a usage threshold (rate limit error) or if they are invalid (validation error).

All occurrences of these errors can be observed using the `loki_discarded_samples_total` and `loki_discarded_bytes_total` metrics. The sections below describe the various possible reasons specified in the `reason` label of these metrics.

A related pair of metrics, `loki_mutated_samples_total` and `loki_mutated_bytes_total`, track samples that Loki changed rather than discarded, for example log lines that were truncated instead of rejected. These samples are still ingested, so they are not counted in `loki_discarded_samples_total` or `loki_discarded_bytes_total`.

It is recommended that Loki operators set up alerts or dashboards with these metrics to detect when rate limits or validation errors occur.

## Terminology

- **sample**: a log line with [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/)
- **stream**: samples with a unique combination of labels
- **active stream**: streams that are present in the ingesters - these have recently received log lines within the `chunk_idle_period` period (default: 30m)

## Rate-Limit Errors

Rate-limits are enforced when Loki cannot handle more requests from a tenant.

Rate-limit errors reject data before Loki stores it. The `rate_limited` error rejects the entire request, while `per_stream_rate_limit` and `stream_limit` reject only the affected stream. The tables below list `Sample discarded: No` for these errors because Loki never accepts the samples in the first place, but the request still increments `loki_discarded_samples_total` and `loki_discarded_bytes_total`. Because these errors are retryable, a client can resend the same data after the rate limit condition clears.

### `rate_limited`

This rate limit is enforced when a tenant has exceeded their configured log ingestion rate limit.

One solution if you're seeing samples dropped due to `rate_limited` is simply to increase the rate limits on your Loki cluster. These limits can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. The config options to use are `ingestion_rate_mb` and `ingestion_burst_size_mb`.

Note that you'll want to make sure your Loki cluster has sufficient resources provisioned to be able to accommodate these higher limits. Otherwise your cluster may experience performance degradation as it tries to handle this higher volume of log lines to ingest.

 Another option to address samples being dropped due to `rate_limits` is simply to decrease the rate of log lines being sent to your Loki cluster. Consider collecting logs from fewer targets or setting up [drop stages](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.process/#stagedrop-block) in Alloy to filter out certain log lines. You can also use Alloy's [rate limiting](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.process/#stagelimit-block) to control the volume of logs sent to your Loki cluster.  

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `distributor`           |
| Outcome                 | Request rejected        |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

### `per_stream_rate_limit`

This limit is enforced when a single stream reaches its rate limit.

Each stream has a rate limit applied to it to prevent individual streams from overwhelming the set of ingesters it is distributed to (the size of that set is equal to the `replication_factor` value).

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. The config options to adjust are `per_stream_rate_limit` and `per_stream_rate_limit_burst`. The default values are `3MB` for `per_stream_rate_limit` and `15MB` for `per_stream_rate_limit_burst`.

Another option you could consider to decrease the rate of samples dropped due to `per_stream_rate_limit` is to split the stream that is getting rate limited into several smaller streams. A third option is to use the Alloy [`stage.limit` block](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.process/#stagelimit-block) to limit the rate of samples sent to the stream hitting the `per_stream_rate_limit`.

We typically recommend setting `per_stream_rate_limit` no higher than 5MB, and `per_stream_rate_limit_burst` no higher than 20MB.

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `ingester`              |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

### `stream_limit`

This limit is enforced when a tenant reaches their maximum number of active streams.

Active streams are held in memory buffers in the ingesters, and if this value becomes sufficiently large then it will cause the ingesters to run out of memory.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file.  To increase the allowable active streams, adjust `max_global_streams_per_user`. Alternatively, the number of active streams can be reduced by removing extraneous labels or removing excessive unique label values.

{{< admonition type="note" >}}
Under normal conditions, this limit only blocks the creation of genuinely new streams. Log lines for streams that are already active continue to be accepted.

However, during ingester scaling (autoscaling, rollouts, or ring changes such as a partition-ingester migration), streams can be rebalanced to ingesters that were not previously handling them. The new ingester has no record of that stream in memory, so it treats the next write for that stream as a stream creation and checks it against its local share of the limit. If that share is already used up, for example because the same tenant is also adding new high-cardinality streams during the rebalance, writes to the already-active stream can be rejected with `stream_limit`, even though the tenant has not added new label cardinality for it.

This effect is temporary. It is bounded by the number of ingesters added or restarted, and it typically resolves once the ring is stable and stream ownership is recalculated.
{{< /admonition >}}

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `ingester`              |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

## Validation Errors

Validation errors occur when part of a request violates a validation rule defined by Loki. Unlike rate-limit errors, validation errors discard only the invalid entry or stream. Valid entries in the same request are still ingested. However, the HTTP response is still a `400 Bad Request`, so clients that retry the entire request on a 400 may create duplicate ingestion of the entries that were already accepted.

### `line_too_long`

This error occurs when a log line exceeds the maximum allowable length in bytes. The HTTP response will include the stream to which the offending log line belongs as well as its size in bytes.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. To increase the maximum line size, adjust `max_line_size`.  We recommend that you do not increase this value above 256kb for performance reasons. Alternatively, Loki can be configured to ingest truncated versions of log lines over the length limit by using the `max_line_size_truncate` option. When truncation is enabled, you can use `max_line_size_truncate_identifier` to append a marker string to each truncated log line, so that truncated lines are easy to identify later.

{{< admonition type="note" >}}
When `max_line_size_truncate` is disabled (the default), the offending log line is discarded and counted in `loki_discarded_samples_total` with `reason="line_too_long"`. When `max_line_size_truncate` is enabled, the log line is truncated and still ingested, so it is counted in `loki_mutated_samples_total` and `loki_mutated_bytes_total` instead.
{{< /admonition >}}

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded, unless `max_line_size_truncate` is enabled, in which case the entry is truncated and ingested |
| Retryable               | **No**            |
| Sample discarded        | **Yes**, unless `max_line_size_truncate` is enabled |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `invalid_labels`

This error occurs when the labels of a submitted stream can't be parsed. Labels that parse but fail another check are counted under that check's reason instead, such as [`max_label_names_per_series`](#max_label_names_per_series) or [`duplicate_label_names`](#duplicate_label_names).

Loki uses the [same validation rules as Prometheus](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels) for validating labels.

> Label names may contain ASCII letters, numbers, as well as underscores. They must match the regex `[a-zA-Z_][a-zA-Z0-9_]*`. Label names beginning with __ are reserved for internal use.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | No                |
| HTTP status code        | `400 Bad Request` |

### `missing_labels`

This validation error is returned when a stream is submitted without any labels.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | No                |
| HTTP status code        | `400 Bad Request` |

### `too_far_behind`

The `too_far_behind` validation error is returned when lines in question are older than half of `max_chunk_age` compared to the newest line in the stream.

{{< admonition type="note" >}}
Loki always accepts out-of-order writes; the per-tenant `unordered_writes` setting and the `-ingester.unordered-writes` flag that used to control this have been removed. `too_far_behind` is the only discard reason for entries that are older than the configured window relative to the stream's newest timestamp.
{{< /admonition >}}

For more details, refer to [Accept out-of-order writes](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#accept-out-of-order-writes).

`max_chunk_age` is a global-only configuration in the `ingester` block (CLI flag `-ingester.max-chunk-age`) and cannot be set per tenant.

This problem can be solved by ensuring that log delivery is configured correctly, or by increasing the `max_chunk_age` value.

We recommend keeping the default value of `max_chunk_age` because changing it affects other behaviors. Instead, try to find the cause of delayed log delivery. This is a per-stream error, so splitting streams by adding more labels can work around it, especially if multiple hosts are sending samples for a single stream.

| Property                | Value               |
|-------------------------|---------------------|
| Enforced by             | `ingester`          |
| Outcome                 | Entry discarded      |
| Retryable               | **No**              |
| Sample discarded        | **Yes**             |
| Configurable per tenant | No, `max_chunk_age` is global only. |
| HTTP status code        | `400 Bad Request`   |

### `greater_than_max_sample_age`

If the `reject_old_samples` config option is set to `true` (it is by default), then samples will be rejected with `reason=greater_than_max_sample_age` if they are older than the `reject_old_samples_max_age` value. You should not see samples rejected for `reason=greater_than_max_sample_age` if `reject_old_samples=false`.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `reject_old_samples_max_age` value, or investigating why log delivery is delayed for this particular stream. The stream in question will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded   |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `too_far_in_future`

If a sample's timestamp is greater than the current timestamp, Loki allows for a certain grace period during which samples will be accepted. If the grace period is exceeded, the error will occur.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `creation_grace_period` value, or investigating why this particular stream has a timestamp too far into the future. The stream in question will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded   |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `max_label_names_per_series`

If a sample is submitted with more labels than Loki has been configured to allow, it will be rejected with the `max_label_names_per_series` reason. Note that 'series' is the same thing as a 'stream' in Loki - the 'series' term is a legacy name.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_names_per_series` value. The stream to which the offending sample (i.e. the one with too many label names) belongs will be returned in the body of the HTTP response.

{{< admonition type="note" >}}
The automatically added `service_name` label is excluded from this count, so a stream may have one more label than `max_label_names_per_series` if one of its labels is `service_name`.
{{< /admonition >}}

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `label_name_too_long`

If a sample is sent with a label name that has a length in bytes greater than Loki has been configured to allow, it will be rejected with the `label_name_too_long` reason.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_name_length` value, though we do not recommend raising it significantly above the default value of `1024` for performance reasons. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `label_value_too_long`

If a sample has a label value with a length in bytes greater than Loki has been configured to allow, it will be rejected for the `label_value_too_long` reason.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_value_length` value. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `duplicate_label_names`

If a sample is sent with two or more identical labels, it will be rejected for the `duplicate_label_names` reason.

The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | No                |
| HTTP status code        | `400 Bad Request` |

### `request_body_too_large`

This error occurs when the raw push request exceeds a size limit before Loki can parse individual streams and log lines out of it. Unlike the other validation errors on this page, `request_body_too_large` can be triggered in two different ways:

- The compressed or decompressed request body exceeds the `max_push_size` setting in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block. The same value is used as the limit for both the compressed and decompressed size. `max_push_size` defaults to `2GB` and can be set per tenant in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file.
- A single stream's label string exceeds 16MB. This limit is hardcoded and cannot be changed through any configuration option. It exists because the Prometheus label parser cannot handle labels above this size.

To resolve this error, reduce the size of the push request (for example by sending fewer streams per request, or by reducing the number or size of labels on a stream), or increase `max_push_size` if your cluster has the resources to handle larger requests.

{{< admonition type="note" >}}
This reason only appears in the `loki_discarded_bytes_total` metric, not in `loki_discarded_samples_total`. Loki rejects the request before it can parse the body, so it cannot count how many log lines the request contained.
{{< /admonition >}}

| Property                | Value                          |
|-------------------------|--------------------------------|
| Enforced by             | `distributor`                  |
| Outcome                 | Request rejected               |
| Retryable               | **No**                         |
| Sample discarded        | **Yes**                        |
| Configurable per tenant | Yes, for `max_push_size`. No, for the hardcoded 16MB label-size limit. |
| HTTP status code        | `413 Request Entity Too Large` |

### `disallowed_structured_metadata`

This error occurs when a stream includes [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) but the tenant is not allowed to send it.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. To allow structured metadata, set `allow_structured_metadata` to `true` (this is the default). The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded   |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `structured_metadata_too_large`

This error occurs when the [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) attached to a single log line exceeds the maximum allowable size in bytes.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. To increase the limit, adjust `max_structured_metadata_size` (default: `64kb`). The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded   |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `structured_metadata_too_many`

This error occurs when a single log line includes more [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) entries than Loki has been configured to allow.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. To increase the limit, adjust `max_structured_metadata_entries_count` (default: `128`). The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Entry discarded   |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

### `blocked_ingestion` and `blocked_ingestion_policy`

{{< admonition type="note" >}}
This is an experimental feature.
{{< /admonition >}}

These reasons occur when an operator has deliberately blocked ingestion for a tenant (`blocked_ingestion`) or for a specific policy (`blocked_ingestion_policy`), rather than because a client sent invalid or excessive data. Policies are defined using the `policy_stream_mapping` setting in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block.

This is configured using `block_ingestion_until` (for a tenant) or `block_ingestion_policy_until` (for a policy), both of which take a date in RFC3339 format. Ingestion is blocked until that date. These values can be set globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, but they are normally set for a single tenant in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file, because a global value blocks ingestion for every tenant.

The `block_ingestion_status_code` option controls what HTTP status code Loki returns while ingestion is blocked. The default is a custom status code, `260`, so that blocked requests can be distinguished from ordinary errors. If `block_ingestion_status_code` is set to `200`, Loki silently drops the blocked data without returning an error to the client, although the data is still counted in `loki_discarded_samples_total` and `loki_discarded_bytes_total`.

| Property                | Value                  |
|-------------------------|------------------------|
| Enforced by             | `distributor`          |
| Outcome                 | Stream rejected, or silently dropped if `block_ingestion_status_code` is `200`; other streams in the same request are unaffected |
| Retryable               | **No**                 |
| Sample discarded        | **Yes**                |
| Configurable per tenant | Yes                    |
| HTTP status code        | `260` by default, configurable with `block_ingestion_status_code` |

### `missing_enforced_labels`

{{< admonition type="note" >}}
This is an experimental feature.
{{< /admonition >}}

This error occurs when a tenant has been configured to require certain labels on every stream, and a stream is submitted that is missing one or more of them.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. Use `enforced_labels` to require labels on all streams, or `policy_enforced_labels` to require them only on streams matching a specific policy defined by `policy_stream_mapping`. The stream and the missing labels will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Stream rejected; other streams in the same request are unaffected |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |
