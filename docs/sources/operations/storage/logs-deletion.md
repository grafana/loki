---
title: Log entry deletion
menuTitle: Log entry deletion
description: Describes how Loki implements log deletion and deletion configuration options.
weight: 700
---
# Log entry deletion

Grafana Loki supports the deletion of log entries from a specified stream.
Log entries that fall within a specified time window and match an optional line filter are those that will be deleted.

Log entry deletion is supported _only_ when TSDB or BoltDB shipper is configured as the index store.

The compactor component exposes REST [endpoints](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api#compactor) that process delete requests.
Hitting the endpoint specifies the streams and the time window.
The deletion of the log entries takes place after a configurable cancellation time period expires.

Log entry deletion relies on configuration of the custom logs retention workflow as defined for the [compactor](../retention/#compactor). The compactor looks at unprocessed requests which are past their cancellation period to decide whether a chunk is to be deleted or not.

## Configuration

Enable log entry deletion by setting `retention_enabled` to true in the compactor's configuration and setting and `deletion_mode` to `filter-only` or `filter-and-delete` in the runtime config.
`delete_request_store` also needs to be configured when retention is enabled to process delete requests, this determines the storage bucket that stores the delete requests.

{{< admonition type="warning" >}}
Be very careful when enabling retention. It is strongly recommended that you also enable versioning on your objects in object storage to allow you to recover from accidental misconfiguration of a retention setting. If you want to enable deletion but not not want to enforce retention, configure the `retention_period` setting with a value of `0s`.
{{< /admonition >}}

Because it is a runtime configuration, `deletion_mode` can be set per-tenant, if desired.

With `filter-only`, log lines matching the query in the delete request are filtered out when querying Loki. They are not removed from storage.
With `filter-and-delete`, log lines matching the query in the delete request are filtered out when querying Loki, and they are also removed from storage.

A delete request may be canceled within a configurable cancellation period. Set the `delete_request_cancel_period` in the compactor's YAML configuration or on the command line when invoking Loki. Its default value is 24h.

As long as the `compactor.retention_enabled` setting is `true`, the API endpoints will be available. Afterwards, access to the deletion API can be enabled per tenant via the `deletion_mode` tenant override.
