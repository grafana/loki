---
title: Log entry deletion
menuTitle: Log entry deletion
description: Describes how Loki implements log deletion and deletion configuration options.
weight: 700
---
# Log entry deletion

Grafana Loki supports the deletion of log entries from a specified stream.
Log entries that fall within a specified time window and match an optional line filter are those that will be deleted.

Log entry deletion is supported when the TSDB index is configured as the index store. It is also supported on the deprecated BoltDB Shipper index, but BoltDB Shipper is being removed in Loki 4.0, so new deployments should use TSDB.

The compactor component exposes REST [endpoints](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#compactor) that process delete requests.
Hitting the endpoint specifies the streams and the time window.
The deletion of the log entries takes place after a configurable cancellation time period expires.

Log entry deletion relies on configuration of the custom logs retention workflow as defined for the [compactor](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/#compactor). The compactor looks at unprocessed requests which are past their cancellation period to decide whether a chunk is to be deleted or not.

## Configuration

Enable log entry deletion by setting `retention_enabled` to `true` in the compactor's configuration (`-compactor.retention-enabled` on the command line). The tenant's `deletion_mode` must also be `filter-only` or `filter-and-delete`, which is the case by default.
`delete_request_store` also needs to be configured when retention is enabled to process delete requests, this determines the storage bucket that stores the delete requests.

{{< admonition type="warning" >}}
Be very careful when enabling retention. It is strongly recommended that you also enable versioning on your objects in object storage to allow you to recover from accidental misconfiguration of a retention setting. If you want to enable deletion but do not want to enforce retention, configure the `retention_period` setting with a value of `0s`.
{{< /admonition >}}

`deletion_mode` is a global and per-tenant setting in [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config). Its default value is `filter-and-delete`, so log entry deletion is active for every tenant as soon as `retention_enabled` is `true` and `delete_request_store` is configured, unless you explicitly change the mode. Set `deletion_mode` in the main configuration file for a global default, or override it per tenant in the [runtime configuration file](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file).

`deletion_mode` supports three values:

- `disabled`: Deletion is not allowed. Requests to the deletion API endpoints are rejected with a `403 Forbidden` response.
- `filter-only`: Log lines matching the query in the delete request are filtered out when querying Loki. They are not removed from storage.
- `filter-and-delete`: Log lines matching the query in the delete request are filtered out when querying Loki, and they are also removed from storage. This is the default.

A delete request may be canceled within a configurable cancellation period. Set the `delete_request_cancel_period` in the compactor's YAML configuration or on the command line when invoking Loki. Its default value is 24h. To cancel a delete request that has already started processing, pass the `force=true` query parameter to the cancellation endpoint.

Delete requests that include a line filter are split into smaller requests that each cover no more than `delete_max_interval` (24h by default). You can request a smaller split for a single request with the `max_interval` query parameter, but it cannot be larger than `delete_max_interval`. Delete requests without a line filter are not split. Refer to the [HTTP API reference](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/#request-log-deletion) for the full set of request parameters.

Delete requests themselves are stored using the database type set by `delete_request_store_db_type`, which defaults to `boltdb`. You can instead use `sqlite`. When you migrate from one database type to another, you can set `backup_delete_request_store_db_type` to `boltdb` so that delete requests are also written to a backup database. Refer to the [compactor configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#compactor) for details.

The deletion API endpoints are registered only when `compactor.retention_enabled` is `true`. When retention is not enabled, the endpoints are unavailable for every tenant, whatever the `deletion_mode` value is. When retention is enabled, use the `deletion_mode` tenant override to control which tenants can use the deletion API.

{{< admonition type="note" >}}
Deletion of log lines with line filters is one of the compactor's most resource-intensive operations. If you delete large volumes of data with line filters, refer to [Horizontal scaling of Compactor](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/compactor-horizontal-scaling/) to distribute that work across multiple compactor instances.
{{< /admonition >}}
