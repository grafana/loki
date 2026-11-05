---
title: Upgrade Loki
menuTitle:  Upgrade
description: Upgrading Grafana Loki
aliases:
  - ../upgrading/
weight: 400
---

# Upgrade Loki

Every attempt is made to keep Grafana Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes we are forced to make decisions between ease of use and ease of maintenance.

If we have any expectation of difficulty upgrading, we will document it here.

As more versions are released it becomes more likely unexpected problems arise moving between multiple versions at once.
If possible try to stay current and do sequential updates. If you want to skip versions, try it in a development environment before attempting to upgrade production.

## Checking for config changes

<!-- vale Grafana.Spelling = NO -->

Using docker you can check changes between 2 versions of Loki with a command like this:

```bash
export OLD_LOKI=2.9.4
export NEW_LOKI=3.0.0
export CONFIG_FILE=local-config.yaml
diff --color=always --side-by-side <(docker run --rm -t -v "${PWD}":/config grafana/loki:${OLD_LOKI} -config.file=/etc/loki/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') <(docker run --rm -t -v "${PWD}":/config grafana/loki:${NEW_LOKI} -config.file=/etc/loki/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') | less -R
```

The `tr -d '\r'` is likely not necessary for most people, seems like WSL2 was sneaking in some windows newline characters...

The output is incredibly verbose as it shows the entire internal config struct used to run Loki, you can play around with the diff command if you prefer to only show changes or a different style output.

## Main / Unreleased

### Ingester WAL entries records have a new version

Ingesters can write WAL entries records as `WALRecordEntriesV4`, which adds the timestamp of the
Kafka record the entries were ingested from. It's disabled by default. To enable it, set
`-ingester.wal-record-v4-enabled` (`ingester.wal.record_v4_enabled`). When it's
disabled, ingesters write `WALRecordEntriesV3` and ignore the timestamps of `WALRecordEntriesV4`
records on replay.

Older versions can't read this record version. If you roll an ingester back after enabling it, it
can't replay the WAL records written since its last checkpoint. It logs
`Recovered from WAL segments with errors`, increments
`loki_ingester_wal_corruptions_total{type="segment"}`, and starts without them, so those entries
are lost.

### Statistics in detected-field query responses

The `/loki/api/v1/detected_fields` and `/loki/api/v1/detected_field/{name}/values` JSON
responses now include a top-level `stats` object containing the underlying log query's
statistics. Existing `fields`, `values`, and `limit` fields retain their format.
Clients that validate response fields strictly must allow the new `stats` field.

### Bloom filters are deprecated

Query acceleration using bloom filters is deprecated and will be removed in a future release.
This covers the Bloom Planner, Bloom Builder, and Bloom Gateway components and the following settings:

- The `bloom_build` and `bloom_gateway` configuration blocks.
- The `storage_config.bloom_shipper` configuration block.
- The per-tenant `bloom_*` settings in `limits_config`, for example `bloom_creation_enabled` and `bloom_gateway_enable_filtering`.

The components and settings still work. Each bloom component logs a deprecation warning on startup.
The `deprecated-config-checker` tool flags the deprecated settings.

### Breaking change: Compactor and retention metrics are renamed 

A number of legacy Prometheus metrics prefixed with `loki_boltdb_shipper_*` are renamed to `loki_tsdb_shipper_*`.
These metrics have already been exposing values from TSDB Shipper and do not change their semantics.

```
loki_boltdb_shipper_compactor_running
loki_boltdb_shipper_compact_tables_operation_*
loki_boltdb_shipper_retention_*
```

To upgrade, check rules and dashboard queries and replace any occurrences of these metrics with their new counterpart with the `loki_tsdb_shipper_*` prefix.

### Stream limit delegation is configurable per tenant

Delegating stream count limits to the ingest-limits service is now a per-tenant limit: `limits_config.delegate_stream_limits_enabled` (`-limits.delegate-stream-limits-enabled`). This is only applicable when running Loki ingestion with Kafka.

The ingester-wide setting `ingester.delegate_stream_limits_enabled` (`-ingester.delegate-stream-limits-enabled`) still works and is deprecated. It provides the default of the per-tenant limit, so existing configurations keep their behavior.

### Optional deferred OTLP attribute expansion

By default, Loki copies OTLP resource and scope attributes configured as structured metadata into each log entry during parsing.

To defer expansion, set `distributor.otlp_config.defer_attribute_expansion` to `true` (`-distributor.otlp.defer-attribute-expansion`). This keeps those attributes shared during distributor processing and Kafka writes until downstream components need them expanded, reducing repeated copying. The option defaults to `false`.

Enabling this option also changes the Kafka record format. Before enabling it, ensure all deployed services consuming log records from Kafka (`partition-ingester`, `dataobj-builder`, and `logline-index-builder`) are running Loki 4.0 or later.

Disabling this option restores expansion during parsing and the previous format for new Kafka writes. Consumers must remain on Loki 4.0 or later for as long as records written while the option was enabled remain in Kafka.

### Stricter JSON payload validation

Both `/loki/api/v1/push` and the deprecated legacy `/api/prom/push` endpoint now return HTTP 400 if a JSON push payload contains a field name that the endpoint's schema does not recognize. For example, sending the legacy `labels`/`entries` payload shape to `/loki/api/v1/push`, or the regular `stream`/`values` payload shape to `/api/prom/push`, now fails with an error containing `found unknown field: <field>`.

### Optional index gateway client request limits

Index gateway clients support two experimental limits that are disabled by default, preserving the existing request limits.

Set `-tsdb.shipper.index-gateway-client.max-retries` to a non-negative value to limit the number of further instances a failed request is retried against. The default of `-1` preserves up to two retries for `GetShards` and retries across all candidate instances for other requests. A value of `0` disables retries. Non-negative values apply to all requests, including `GetShards`. Each candidate instance is tried at most once. Enabling a retry limit bounds how long a request can occupy a goroutine when many instances are slow or unreachable.

The candidate instances are the healthy instances the ring or DNS reports, so an instance that already failed its heartbeat is not counted against the budget. If you leave `-index-gateway.shard-size` at its default of `0`, a request can choose from the whole index gateway fleet.

Set `-tsdb.shipper.index-gateway-client.max-in-flight-requests` to a positive value to limit in-flight requests. The default of `0` disables this limit. Requests that arrive when the limit is reached fail immediately with an HTTP 503 status, so the query-frontend retries them. The limit applies per client: Loki builds one client for each `schema_config` period, so the process-wide limit is this value multiplied by the number of clients.

### `frontend.encoding` default changed to `protobuf`

The default value of `-frontend.encoding` / `frontend.encoding` changed from `json` to `protobuf`. This only affects the internal request/response encoding between the query-frontend, query-scheduler, and querier. Client-facing APIs are unchanged, and no persisted state uses this setting, so no data migration is required.

Schedulers and queriers already accept both encodings, so mixed frontends during a rolling upgrade are safe. To keep the previous behavior, set `frontend.encoding: json` explicitly.

### LogQL rejects numeric, duration, bytes, and `ip()` comparisons against `__error__` and `__error_details__`

A query such as `| __error__ > 0`, `| __error__ != 1s`, `| __error_details__ == 1MB`, or `| __error__ = ip("1.2.3.4")` now fails to parse. These two labels always hold a string (or are unset), so a numeric, duration, bytes, or IP comparison against them could never find a match; such a query used to parse successfully and then silently return no results. This also applies after `| unwrap`. Use a string comparison instead, for example `| __error__ != ""` or `| __error__=""`.

### `ip()` label filters no longer keep lines with an `__error__` label

An `ip()` label filter used to keep every log line that already carried an `__error__` label, without checking the address. It now compares the line like a numeric, duration, or bytes filter does. A line that failed in an earlier stage, such as `json`, usually has no value for the label, so the filter drops it. For example, `| json | addr = ip("10.0.0.0/8")` no longer returns the lines that failed to parse, and a metric query with this filter no longer fails on them. To keep those lines, combine the filter with an `__error__` filter, for example `| addr = ip("10.0.0.0/8") or __error__ != ""`.

### Breaking change: Errored samples in metric queries now follow the query's grouping and `__error__` filter

Metric queries report errored log lines in a more consistent way now.

Previously, an errored line's sample kept every stream label and every structured metadata key, ignoring the query's `by (...)` or `without (...)` grouping. Lines from the same stream that differed only in a label the grouping didn't name could form separate series. Now an errored sample reports the same labels a successful line would report for that grouping, plus `__error__` and `__error_details__`. `sum()` and `count()` totals are unchanged, but `first_over_time()`, `last_over_time()`, `quantile_over_time()`, and other range aggregations that accept a grouping can report fewer series, or different values, once errored lines collapse into the group they belong to. A failing query's `pipeline error` message now names the grouped labels instead of the whole label set; remove the grouping to identify the affected stream.

Separately, whether a metric query fails on an errored sample, or returns it, now depends only on whether a `__error__` label filter in the pipeline asks to keep it (for example `| __error__ != ""`). It no longer depends on how the query is grouped: previously, `sum by (pod) (count_over_time({app="a"} | json | __error__!="" [1m]))` could return errored samples while the same pipeline written with `max`, `without (...)`, or no grouping instead failed the query.

For the full set of rules, see [Pipeline Errors in Metric Queries](https://grafana.com/docs/loki/<LOKI_VERSION>/query/query_reference/#pipeline-errors-in-metric-queries).

### `frontend.compress_responses` default changed to `true`

The default value of `frontend.compress_responses` changed to `true`. A bug in Loki 3.4.0 unintentionally switched it to `false`. If you don't want the query-frontend to compress HTTP responses, set `frontend.compress_responses` to `false` explicitly.

### Breaking change: Removal of LogQL `variants()` queries

The experimental `variants()` LogQL expression is no longer supported.

The per-tenant setting `enable_multi_variant_queries` (`-limits.enable-multi-variant-queries`) that gated it has been removed. A leftover `enable_multi_variant_queries:` key in `limits_config` or in a runtime overrides file is ignored, so it does not block an upgrade, but you should remove it; the `deprecated-config-checker` tool will flag it. The `-limits.enable-multi-variant-queries` command line flag no longer exists and Loki fails to start if it is passed.

### Breaking change: Removal of the `row_shards` schema setting

The `row_shards` setting on a `schema_config` `period_config` has been removed. It configured a static query shard factor for legacy (non-TSDB) index types. TSDB, the only supported index type, resolves log and metric query sharding dynamically from index statistics and ignores `row_shards`; series queries continue to use the previous default factor of 16. Because schema config is parsed strictly, a leftover `row_shards:` key now fails config load. Remove the `row_shards` setting from every `period_config`; the `deprecated-config-checker` tool will flag it.

### TSDB schema v14

Loki now supports the experimental TSDB storage schema `v14`. Schema v14 uses the
same chunk format as v13 and changes only the TSDB index format, which adds a
per-chunk ingestion timestamp used by ingestion-time retention features.

v13 remains the recommended schema. To opt in to v14, add a new `period_config`
with a future `from` date and `schema: v14`; existing v13 periods are unaffected.

Before configuring any v14 period, upgrade all components to a version that can
read the v14 index format. Rolling back after v14 data has been written requires
stopping new v14 writes first, because earlier binaries cannot read v14 indexes.

### TSDB head WAL chunk records use a new binary format

The ingester writes chunk records to the TSDB head write-ahead log (WAL) in a new format that stores each chunk's ingestion timestamp to support [TSDB schema v14](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#tsdb-schema-v14).
Upgrading needs no action, because Loki still reads the old format.
Once upgraded, previously released Loki versions (3.7.x and earlier) cannot read the new format:
The WAL replay fails with `error recovering head from TSDB WAL: unknown record type`, Loki does not treat this as WAL corruption, and the ingester fails to start.

To roll back after the new format was written, remove the WAL data before you start the older binary:

1. Stop each ingester gracefully. A successful shutdown flushes chunks, builds the in-memory head into index files, and truncates the head WAL, so the older binary starts with an empty WAL directory.
1. If an ingester crashed or was killed before finishing that work, delete its head WAL directory, the `wal` subdirectory of `storage_config.tsdb_shipper.active_index_directory`.

Deleting the WAL discards index entries for chunks flushed since the ingester last built an index from its head, which can cover up to one index period.
Those chunks stay in object storage but no index references them, so their logs are not queryable.
Index entries from the other replicas still cover the same log data, unless you delete the WAL on every replica.

### Breaking change: Thanos storage clients are used by default

The default value of `storage_config.use_thanos_objstore` changed from `false` to `true`, enabling the Thanos-based object store clients by default if not otherwise explicitly specified.

Please refer to [Migrate to Thanos storage clients](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-storage-clients/) for how to migrate your configuration.

### Breaking change: Fully remove Simple Scalable Deployment (SSD) mode

Simple Scalable Deployment (SSD) mode is being deprecated and removed in Loki 4.0. The targets `write`, `read`, and `backend`, as well as the configuration option `-legacy-read-mode` are not available any more and Loki will fail to start if used.

For the best possible experience in production, we recommend deploying Loki in distributed mode. Please refer to the [Migrating from SSD to distributed](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/ssd-to-distributed/) guide for instructions how to migrate your deployment to distributed mode.

A second option for smaller scale deployments that still need high availability, is to migrate to HA Monolithic, which reduces the complexity of the deployment. Please refer to the [Migrate from SSD to HA Monolithic](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/ssd-to-ha-monolithic/) guide for instructions how to migrate your deployment.

### Breaking change: Ruler always wipes its remote-write WAL on startup

The ruler now deletes (wipes) its remote-write write-ahead log (WAL) directory on startup and starts each tenant with a fresh, empty WAL. WAL data left over from a previous run is no longer replayed, so any samples that had not yet been flushed to remote storage before a restart are discarded. For recording rules this is generally safe, since rules are re-evaluated and re-sent after startup.

As a result, the following have been removed:

- The `-ruler.enable-wal-replay` flag and its per-tenant equivalent `ruler_enable_wal_replay` (in `limits_config`). The ruler no longer replays the WAL, so these settings no longer have any effect. Remove them from your configuration; the `deprecated-config-checker` tool will flag them.
- The ruler WAL metrics that only ever reported replay or repair activity: `loki_ruler_wal_corruptions_total`, `loki_ruler_wal_corruptions_repair_failed_total`, `loki_ruler_wal_corruptions_repair_succeeded_total`, and `loki_ruler_wal_replay_duration`. Remove any dashboards or alerts that reference them.

### Breaking change: Removal of various deprecated configuration options

- The per-tenant ruler remote write settings have been removed in favor of `limits_config.ruler_remote_write_config`:
  - `limits_config.ruler_remote_write_url`
  - `limits_config.ruler_remote_write_timeout`
  - `limits_config.ruler_remote_write_headers`
  - `limits_config.ruler_remote_write_relabel_configs`
  - `limits_config.ruler_remote_write_queue_capacity`
  - `limits_config.ruler_remote_write_queue_min_shards`
  - `limits_config.ruler_remote_write_queue_max_shards`
  - `limits_config.ruler_remote_write_queue_max_samples_per_send`
  - `limits_config.ruler_remote_write_queue_batch_send_deadline`
  - `limits_config.ruler_remote_write_queue_min_backoff`
  - `limits_config.ruler_remote_write_queue_max_backoff`
  - `limits_config.ruler_remote_write_queue_retry_on_ratelimit`
  - `limits_config.ruler_remote_write_sigv4_config`
- The single remote-write `client` setting (`ruler.remote_write.client` in the yaml file) has been removed in favor of the `clients` map (`ruler.remote_write.clients`), which allows configuring one or more remote-write clients keyed by an id. When upgrading, move your existing `client` configuration under `clients` with a chosen key.
- The settings `-limits.per-user-override-config` (`limits_config.per_tenant_override_config`) and `-limits.per-user-override-period` (`limits_config.per_tenant_override_period`) have been removed in favor of `-runtime-config.file` (`runtime_config.file`) and `-runtime-config.reload-period` (`runtime_config.period`) respectively.
- The per-tenant setting `unordered_writes` has been removed. Loki now always allows unordered writes.
- The setting `-store.index-cache-write` (`chunk_store_config.write_dedupe_cache_config` block in the yaml file) has been removed as it was only used for legacy storage backends that have been removed as well.
- The setting `-store.index-cache-read` (`storage_config.index_queries_cache_config` block in the yaml file) has been removed as it was only used for legacy storage backends (`boltdb-shipper`) that have been removed as well.
- The setting `-store.index-cache-validity` (`storage_config.index_cache_validity` block in the yaml file) has been removed as it was only used in combination with the removed `-store.index-cache-read` setting.
- The setting `-store.cache-lookups-older-than` (`chunk_store_config.cache_lookups_older_than` in the yaml file) has been removed as it was only used to cache index entries for legacy storage backends that have been removed as well.
- The setting `-store.disable-broad-index-queries` (`storage_config.disable_broad_index_queries` in the yaml file) has been removed as it was only used for legacy storage backends that have been removed as well.
- The `chunks` block (`chunks:` under a `schema_config` `period_config` in the yaml file) has been removed as it only configured chunk tables for legacy storage backends that have been removed as well. TSDB stores chunks directly in object storage. Remove the `chunks` block from every `period_config`.
- The `tags` setting (`tags:` under the `index` block of a `schema_config` `period_config` in the yaml file) has been removed as it only applied to managed tables of legacy storage backends that have been removed as well. Remove the `tags` setting from every `period_config`.

Use the `deprecated-config-checker` tool to validate your `config.yaml`.

### Breaking change: Configure deletes on compactor

The configuration option `-compactor.allow-deletes` has been removed. Instead, use the the per-tenant `deletion_mode` option instead.
This is configured in the `limits_config` and can be one of `disabled`, `filter-only`, or `filter-and-delete`.
When set to `filter-only` or `filter-and-delete`, and `retention_enabled` is set to true, then the log entry deletion API endpoints are available.

### Breaking change: Rename and remove metrics

- The deprecated metric `loki_log_messages_total` is removed in favor of `loki_internal_log_messages_total`.
- The metric `loki_log_flushes` is renamed to `loki_internal_log_flushes` to be consistent with `loki_internal_log_messages_total`.
- The second label of the `loki_mutated_samples_total` and `loki_mutated_bytes_total` metrics is renamed from `truncated` to `tenant`. The label always held the tenant ID (not a boolean), so the new name reflects its actual value and is consistent with the `tenant` label on the `loki_discarded_samples_total` / `loki_discarded_bytes_total` metrics. If you have dashboards or alerts that aggregate these metrics by the `truncated` label, update them to use `tenant`.

### Breaking change: Drop support for non-TSDB stores in jsonnet lib 

With the removal of deprecated storage backends, the Loki jsonnet library is also cleaned up to reflect these changes. Affected configuration flags are:

- `$._config.using_shipper_store` - any usages defaulted to `true`
- `$._config.using_boltdb_shipper` - any usages defaulted to `false`
- `$._config.using_tsdb_shipper` - any usages defaulted to `true`

This change may update both command line arguments and the Loki config. If you've been using or overriding one of the three aforementioned configuration options, please remove them and replace them with the new defaults.

### Breaking change: Removal of deprecated storage backends

We deprecated legacy storage backends in Loki 3.0 and now they are subsequently removed:
- Google BigTable (for chunks and indexes)
- Apache Cassandra (for chunks and indexes)
- Amazon DynamoDB (for indexes)
- gRPC Store (for chunks and indexes)
- BoltDB (for indexes)

Loki will fail to start if a deprecated and removed storage backend is referenced in the schema or storage configuration. You must not upgrade if you still use one of the above mentioned backends.

Please refer to [Storage schema](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/schema/) for more information about how to evolve your schema.

If the latest entry of your schema config is older than retention period of your data, then it is safe to remove any old entries from the `schema_config.configs` when upgrading.

With the legacy backends removed, also the `table-manager` target and `table_manager` configuration block are removed, as they are not needed any more. If you have a `table_manager` configuration block in your `config.yaml` you can safely remove it completely.

### Distributor Max Receive Limits for uncompressed bytes

The next Loki release introduces a new configuration option (i.e. `-distributor.max-recv-msg-size`) for the distributors to control the max receive size of uncompressed stream data. The new options's default value is set to `100MB`.

Supported clients should check the configuration options for max send message size if applicable.

### Ruler WAL directory now honors `common.path_prefix`

The default `-ruler.wal.dir` (`ruler-wal`) is now composed against `common.path_prefix` when one is set, matching the behavior of the other path-bearing defaults already rewritten by Loki: `-ruler.rule-path`, `-ingester.wal-dir`, `-compactor.working-directory`, and `-bloom.shipper.working-directory`. Previously the ruler WAL stayed at the cwd-relative `ruler-wal`, which fails on `mkdir` in read-only-rootfs containers (#7377).

Deployments that set `common.path_prefix` but did not explicitly set `-ruler.wal.dir` will see the ruler create a new WAL directory at `<path_prefix>/ruler-wal` after the upgrade. The previous cwd-relative `ruler-wal` directory is not migrated; any un-remote-written recording-rule samples buffered there are dropped on first restart.

To preserve the previous location, set `-ruler.wal.dir` explicitly to the old path (e.g. `ruler-wal` or its absolute equivalent) in your config before upgrading. Deployments that already set `-ruler.wal.dir` explicitly are unaffected.

## Further reading

To upgrade from one of the versions of Loki 3.x, refer to [Upgrade Loki 3.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-3.x/).

To upgrade from one of the versions of Loki 2.x, refer to [Upgrade Loki 2.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-2.x/).

To upgrade from one of the versions of Loki 1.x, refer to [Upgrade Loki 1.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-1.x/).
