---
title: Enforce rate limits and push request validation
menuTitle: Rate limits
description: Decribes the different rate limits and push request validation and their error handling.
weight: 
---
# Enforce rate limits and push request validation

Loki will reject requests if they exceed a usage threshold (rate limit error) or if they are invalid (validation error).

All occurrences of these errors can be observed using the `loki_discarded_samples_total` and `loki_discarded_bytes_total` metrics. The sections below describe the various possible reasons specified in the `reason` label of these metrics.

It is recommended that Loki operators set up alerts or dashboards with these metrics to detect when rate limits or validation errors occur. 


### Terminology

- **sample**: a log line with [structured metadata](../../get-started/labels/structured-metadata/)
- **stream**: samples with a unique combination of labels 
- **active stream**: streams that are present in the ingesters - these have recently received log lines within the `chunk_idle_period` period (default: 30m)

## Rate-Limit Errors

Rate-limits are enforced when Loki cannot handle more requests from a tenant.

### `rate_limited`

This rate limit is enforced when a tenant has exceeded their configured log ingestion rate limit.

One solution if you're seeing samples dropped due to `rate_limited` is simply to increase the rate limits on your Loki cluster. These limits can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. The config options to use are `ingestion_rate_mb` and `ingestion_burst_size_mb`.

Note that you'll want to make sure your Loki cluster has sufficient resources provisioned to be able to accommodate these higher limits. Otherwise your cluster may experience performance degradation as it tries to handle this higher volume of log lines to ingest.

 Another option to address samples being dropped due to `rate_limits` is simply to decrease the rate of log lines being sent to your Loki cluster. Consider collecting logs from fewer targets or setting up `drop` stages in Promtail to filter out certain log lines. Promtail's [limits configuration](/docs/loki/<LOKI_VERSION>/send-data/promtail/configuration/#limits_config) also gives you the ability to control the volume of logs Promtail remote writes to your Loki cluster.  


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

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. The config options to adjust are `per_stream_rate_limit` and `per_stream_rate_limit_burst`.

Another option you could consider to decrease the rate of samples dropped due to `per_stream_rate_limit` is to split the stream that is getting rate limited into several smaller streams. A third option is to use Promtail's [limit stage](/docs/loki/<LOKI_VERSION>/send-data/promtail/stages/limit/#limit-stage) to limit the rate of samples sent to the stream hitting the `per_stream_rate_limit`. 

We typically recommend setting `per_stream_rate_limit` no higher than 5MB, and `per_stream_rate_limit_burst` no higher than 20MB.

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `ingester`              |
| Outcome                 | Request rejected        |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

### `stream_limit`

This limit is enforced when a tenant reaches their maximum number of active streams.

Active streams are held in memory buffers in the ingesters, and if this value becomes sufficiently large then it will cause the ingesters to run out of memory.

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file.  To increase the allowable active streams, adjust `max_global_streams_per_user`. Alternatively, the number of active streams can be reduced by removing extraneous labels or removing excessive unique label values.

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `ingester`              |
| Outcome                 | Request rejected        |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

## Validation Errors

Validation errors occur when a request violates a validation rule defined by Loki.

### `line_too_long`

This error occurs when a log line exceeds the maximum allowable length in bytes. The HTTP response will include the stream to which the offending log line belongs as well as its size in bytes. 

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. To increase the maximum line size, adjust `max_line_size`.  We recommend that you do not increase this value above 256kb for performance reasons. Alternatively, Loki can be configured to ingest truncated versions of log lines over the length limit by using the `max_line_size_truncate` option.

| Property                | Value            |
|-------------------------|------------------|
| Enforced by             | `distributor`    |
| Retryable               | **No**           |
| Sample discarded        | **Yes**          |
| Configurable per tenant | Yes              |

### `invalid_labels`

This error occurs when one or more labels in the submitted streams fail validation.

Loki uses the [same validation rules as Prometheus](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels) for validating labels.

> Label names may contain ASCII letters, numbers, as well as underscores. They must match the regex `[a-zA-Z_][a-zA-Z0-9_]*`. Label names beginning with __ are reserved for internal use.

| Property                | Value         |
|-------------------------|---------------|
| Enforced by             | `distributor` |
| Retryable               | **No**        |
| Sample discarded        | **Yes**       |
| Configurable per tenant | No            |

## `missing_labels`

This validation error is returned when a stream is submitted without any labels.

| Property                | Value         |
|-------------------------|---------------|
| Enforced by             | `distributor` |
| Retryable               | **No**        |
| Sample discarded        | **Yes**       |
| Configurable per tenant | No            |

## `too_far_behind` and `out_of_order`

The `too_far_behind` and `out_of_order` reasons are identical. Loki clusters with `unordered_writes=true` (the default value as of Loki v2.4) use `reason=too_far_behind`. Loki clusters with `unordered_writes=false` use `reason=out_of_order`.

This validation error is returned when a stream is submitted out of order. More details can be found [here](/docs/loki/<LOKI_VERSION>/configuration/#accept-out-of-order-writes) about the Loki ordering constraints.

The `unordered_writes` config value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file, whereas `max_chunk_age` is a global configuration.

This problem can be solved by ensuring that log delivery is configured correctly, or by increasing the `max_chunk_age` value.

It is recommended to resist modifying the default value of `max_chunk_age` as this has other implications, and to instead try track down the cause for delayed logged delivery. It should also be noted that this a per-stream error, so by simply splitting streams (adding more labels) this problem can be circumvented, especially if multiple hosts are sending samples for a single stream.

| Property                | Value      |
|-------------------------|------------|
| Enforced by             | `ingester` |
| Retryable               | **No**     |
| Sample discarded        | **Yes**    |
| Configurable per tenant | No         |

## `greater_than_max_sample_age`

If the `reject_old_samples` config option is set to `true` (it is by default), then samples will be rejected with `reason=greater_than_max_sample_age` if they are older than the `reject_old_samples_max_age` value. You should not see samples rejected for `reason=greater_than_max_sample_age` if `reject_old_samples=false`.

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `reject_old_samples_max_age` value, or investigating why log delivery is delayed for this particular stream. The stream in question will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `too_far_in_future`

If a sample's timestamp is greater than the current timestamp, Loki allows for a certain grace period during which samples will be accepted. If the grace period is exceeded, the error will occur.

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `creation_grace_period` value, or investigating why this particular stream has a timestamp too far into the future. The stream in question will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `max_label_names_per_series`

If a sample is submitted with more labels than Loki has been configured to allow, it will be rejected with the `max_label_names_per_series` reason. Note that 'series' is the same thing as a 'stream' in Loki - the 'series' term is a legacy name. 

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_names_per_series` value. The stream to which the offending sample (i.e. the one with too many label names) belongs will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `label_name_too_long`

If a sample is sent with a label name that has a length in bytes greater than Loki has been configured to allow, it will be rejected with the `label_name_too_long` reason. 

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_name_length` value, though we do not recommend raising it significantly above the default value of `1024` for performance reasons. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `label_value_too_long`

If a sample has a label value with a length in bytes greater than Loki has been configured to allow, it will be rejected for the `label_value_too_long` reason. 

This value can be modified globally in the [`limits_config`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](/docs/loki/<LOKI_VERSION>/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_value_length` value. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `duplicate_label_names`

If a sample is sent with two or more identical labels, it will be rejected for the `duplicate_label_names` reason. 

The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | No                |
| HTTP status code        | `400 Bad Request` |
