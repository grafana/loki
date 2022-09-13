---
title: Request Validation & Rate-Limit Errors
weight: 30
---

# Request Validation & Rate-Limit Errors

Loki will reject requests if they exceed a usage threshold (rate-limit error) or if they are invalid (validation error).

All occurrences of these errors can be observed using the `loki_discarded_samples_total` and `loki discarded_bytes_total` metrics. The sections below describe the various possible reasons specified in the `reason` label of these metrics.

It is recommended that Loki operators set up alerts or dashboards with these metrics to detect when rate-limits or validation errors occur. 

If you are using Grafana Cloud, these can be observed in your Grafana instance in the _Grafana Cloud Billing/Usage_ dashboard under the _Discarded Log Samples_ panel.

### Terminology

- **sample**: a log line
- **series**/**stream**: samples with a unique combination of labels
- **active stream**: streams that have received log lines within the last `max_chunk_age` period (default: 2h)

## Rate-Limit Errors

Rate-limits are enforced when Loki cannot handle more requests from a tenant.

### `rate_limited`

This rate-limit is enforced when a tenant has exceeded their configured log ingestion rate-limit.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. The config options to use are `ingestion_rate_mb` and `ingestion_burst_size_mb`.

| Property                | Value                   |
|-------------------------|-------------------------|
| Enforced by             | `distributor`           |
| Outcome                 | Request rejected        |
| Retryable               | Yes                     |
| Sample discarded        | No                      |
| Configurable per tenant | Yes                     |
| HTTP status code        | `429 Too Many Requests` |

### `per_stream_rate_limit`

This limit is enforced a stream reaches its rate-limit.

Each stream has a rate-limit applied to it to prevent individual streams from overwhelming a set of ingesters (equal to the `replication_factor` value).

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. The config options to adjust are `per_stream_rate_limit` and `per_stream_rate_limit_burst`.

In Grafana Cloud, we typically set `per_stream_rate_limit` no higher than 5MB, and `per_stream_rate_limit_burst` no higher than 20MB.

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

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file.  To increase the allowable active streams, adjust `max_global_streams_per_user`. Alternatively, the number of active streams can be reduced by removing extraneous labels or removing excessive unique label values.

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

This error occurs when a log line exceeds the maximum allowable length in bytes.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. To increase the maximum line size, adjust `max_line_size`.  Alternatively, logs can be truncated if they exceed the maximum length in bytes with `max_line_size_truncate`.

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

The `too_far_behind` and `out_of_order` reasons are identical, except that `out_of_order` can only occur if the `unordered_writes` config option is set to `false`. The default value since Loki v2.4 is `true`, so `too_far_behind` is more common.

This validation error is returned when a stream is submitted out of order. More details can be found [here](https://grafana.com/docs/loki/latest/configuration/#accept-out-of-order-writes) about Loki's ordering constraints.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block.

This problem can be solved by ensuring that log delivery is configured correctly, or by increasing the `max_chunk_age` value.

It is recommended to resist configuring the `max_chunk_age` option as this has other implications, and to instead try track down the cause for delayed logged delivery. It should also be noted that this a per-stream error, so by simply splitting streams (adding more labels) this problem can be circumvented, especially if multiple hosts are sending samples for a single stream.

| Property                | Value      |
|-------------------------|------------|
| Enforced by             | `ingester` |
| Retryable               | **No**     |
| Sample discarded        | **Yes**    |
| Configurable per tenant | No         |

## `greater_than_max_sample_age`

If the `reject_old_samples` config option is set to `true` (it is by default), then samples will be rejected if they are older than the `reject_old_samples_max_age` value.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `reject_old_samples_max_age` value, or investigating why log delivery is delayed for this particular stream. The stream in question will be returned in the body of the HTTP response.

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

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `creation_grace_period` value, or investigating why this particular stream has a timestamp too far into the future. The stream in question will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `max_label_names_per_series`

If a series is submitted with more labels than Loki has been configured to allow, this error will occur.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_names_per_series` value. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `label_name_too_long`

If a series has a label with a length in bytes greater than Loki has been configured to allow, this error will occur.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_name_length` value. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `label_value_too_long`

If a series has a label with a length in bytes greater than Loki has been configured to allow, this error will occur.

This value can be modified globally in the [`limits_config`](https://grafana.com/docs/loki/latest/configuration/#limits_config) block, or on a per-tenant basis in the [runtime overrides](https://grafana.com/docs/loki/latest/configuration/#runtime-configuration-file) file. This error can be solved by increasing the `max_label_value_length` value. The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | Yes               |
| HTTP status code        | `400 Bad Request` |

## `duplicate_label_names`

If a series has two or more identical labels, this error will occur.

The offending stream will be returned in the body of the HTTP response.

| Property                | Value             |
|-------------------------|-------------------|
| Enforced by             | `distributor`     |
| Outcome                 | Request rejected  |
| Retryable               | **No**            |
| Sample discarded        | **Yes**           |
| Configurable per tenant | No                |
| HTTP status code        | `400 Bad Request` |