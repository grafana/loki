---
title: Upgrade Loki 2.x versions
menuTitle: 
description:  Upgrading instructions for Grafana Loki 2.x versions.
weight: 600
---


# Upgrade Loki 2.x versions

Every attempt is made to keep Grafana Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes we are forced to make decisions between ease of use and ease of maintenance.

If we have any expectation of difficulty upgrading, we will document it here.

As more versions are released it becomes more likely unexpected problems arise moving between multiple versions at once. If possible try to stay current and do sequential updates. If you want to skip versions, try it in a development environment before attempting to upgrade production.

The following are upgrade instructions for Grafana Loki 2.x versions.

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

## 2.9.0

### Loki 2.9.0

#### Index gateway shuffle sharding

The index gateway now supports shuffle sharding of index data when running in
"ring" mode. The index data is sharded by tenant where each tenant gets
assigned a sub-set of all available instances of the index gateways in the ring.

If you configured a high replication factor to accommodate for load, since
in the past this was the only option to give a tenant more instances for
querying, you should consider reducing the replication factor to a meaningful
value for replication (for example, from 12 to 3) and instead set the shard factor for
individual tenants as required.

If the global shard factor (no per-tenant) is 0 (default value), the global
shard factor is set to replication factor. It can still be overwritten per
tenant.

In the context of the index gateway, sharding is synonymous to replication.

#### Index shipper multi-store support

In previous releases, if you did not explicitly configure `-boltdb.shipper.shared-store`, `-tsdb.shipper.shared-store`, those values default to the `object_store` configured in the latest `period_config` of the corresponding index type.
These defaults are removed in favor of uploading indexes to multiple stores. If you do not explicitly configure a `shared-store`, the boltdb and tsdb indexes will be shipped to the `object_store` configured for that period.

#### Shutdown marker file

A shutdown marker file can be written by the `/ingester/prepare_shutdown` endpoint.
If the new `ingester.shutdown_marker_path` config setting has a value that value is used.
If not the`common.path_prefix` config setting is used if it has a value. Otherwise a warning is shown
in the logs on startup and the `/ingester/prepare_shutdown` endpoint will return a 500 status code.

#### Compactor multi-store support

In previous releases, setting `-boltdb.shipper.compactor.shared-store` configured the following:

- store used for managing delete requests.
- store on which index compaction should be performed.

If `-boltdb.shipper.compactor.shared-store` was not set, it used to default to the `object_store` configured in the latest `period_config` that uses either the tsdb or boltdb-shipper index.

Compactor now supports index compaction on multiple buckets/object stores.
And going forward loki will not set any defaults on `-boltdb.shipper.compactor.shared-store`, this has a couple of side effects detailed as follows:

##### store on which index compaction should be performed

If `-boltdb.shipper.compactor.shared-store` is configured by the user, loki would run index compaction only on the store specified by the config.
If not set, compaction would be performed on all the object stores that contain either a boltdb-shipper or tsdb index.

##### store used for managing delete requests

A new config option `-boltdb.shipper.compactor.delete-request-store` decides where delete requests should be stored. This new option takes precedence over `-boltdb.shipper.compactor.shared-store`.

In the case where neither of these options are set, the `object_store` configured in the latest `period_config` that uses either a tsdb or boltdb-shipper index is used for storing delete requests to ensure pending requests are processed.

#### logfmt parser non-strict parsing

logfmt parser now performs non-strict parsing which helps scan semi-structured log lines.
It skips invalid tokens and tries to extract as many key/value pairs as possible from the rest of the log line.
If you have a use-case that relies on strict parsing where you expect the parser to throw an error, use `| logfmt --strict` to enable strict mode.

logfmt parser doesn't include standalone keys(keys without a value) in the resulting label set anymore.
You can use `--keep-empty` flag to retain them.

### Jsonnet 2..9.0

#### Deprecated PodDisruptionBudget definition has been removed

The `policy/v1beta1` API version of PodDisruptionBudget is no longer served as of Kubernetes v1.25.
To support the latest versions of the Kubernetes, it was necessary to replace `policy/v1beta1` with the new definition `policy/v1` that is available since v1.21.

No impact is expected if you use Kubernetes v1.21 or newer.

Please refer to [official migration guide](https://kubernetes.io/docs/reference/using-api/deprecation-guide/#poddisruptionbudget-v125) for more details.

## 2.8.0

### Loki 2.8.0

#### Change in LogQL behavior

When there are duplicate labels in a log line, only the first value will be kept. Previously only the last value
was kept.

#### Default retention_period has changed

This change will affect you if you have:

```yaml
compactor:
  retention_enabled: true
```

And did *not* define a `retention_period` in `limits_config`, thus relying on the previous default of `744h`

In this release the default has been changed to `0s`.

A value of `0s` is the same as "retain forever" or "disable retention".

If, **and only if**, you wish to retain the previous default of 744h, apply this config.

```yaml
limits_config:
  retention_period: 744h
```

{{< admonition type="note" >}}
In previous versions, the zero value of `0` or `0s` will result in **immediate deletion of all logs**,
only in 2.8 and forward releases does the zero value disable retention.
{{< /admonition >}}

#### metrics.go log line `subqueries` replaced with `splits` and `shards`

The metrics.go log line emitted for every query had an entry called `subqueries` which was intended to represent the amount a query was parallelized on execution.

In the current form it only displayed the count of subqueries generated with the Loki split by time logic and did not include counts for shards.

There wasn't a clean way to update subqueries to include sharding information and there is value in knowing the difference between the subqueries generated when we split by time vs sharding factors, especially now that TSDB can do dynamic sharding.

In 2.8 we no longer include `subqueries` in metrics.go, it does still exist in the statistics API data returned but just for backwards compatibility, the value will always be zero now.

Instead, now you can use `splits` to see how many split by time intervals were created and `shards` to see the total number of shards created for a query.

{{< admonition type="note" >}}
Currently not every query can be sharded and a shards value of zero is a good indicator the query was not able to be sharded.
{{< /admonition >}}

### Promtail 2.8.0

#### The go build tag `promtail_journal_enabled` was introduced

The go build tag `promtail_journal_enabled` should be passed to include Journal support to the promtail binary.
If you need Journal support you will need to run go build with tag `promtail_journal_enabled`:

```shell
go build --tags=promtail_journal_enabled ./clients/cmd/promtail
```

Introducing this tag aims to relieve Linux/CentOS users with CGO enabled from installing libsystemd-dev/systemd-devel libraries if they don't need Journal support.

### Ruler

#### CLI flag `ruler.wal-cleaer.period` deprecated

CLI flag `ruler.wal-cleaer.period` is now deprecated and replaced with a typo fix `ruler.wal-cleaner.period`.
The yaml configuration remains unchanged:

```yaml
ruler:
  wal_cleaner:
    period: 5s
```

### Querier

#### query-frontend Kubernetes headless service changed to load balanced service

{{< admonition type="note" >}}
This is relevant only if you are using [jsonnet for deploying Loki in Kubernetes](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/tanka/).
{{< /admonition >}}

The `query-frontend` Kubernetes service was previously headless and was used for two purposes:

* Distributing the Loki query requests amongst all the available Query Frontend pods.
* Discover IPs of Query Frontend pods from Queriers to connect as workers.

The problem here is that a headless service does not support load balancing and leaves it up to the client to balance the load.
Additionally, a load-balanced service does not let us discover the IPs of the underlying pods.

To meet both these requirements, we have made the following changes:

* Changed the existing `query-frontend` Kubernetes service from headless to load-balanced to have a fair load distribution on all the Query Frontend instances.
* Added `query-frontend-headless` to discover QF pod IPs from queriers to connect as workers.

If you are deploying Loki with Query Scheduler by setting [query_scheduler_enabled](https://github.com/grafana/loki/blob/cc4ab7487ab3cd3b07c63601b074101b0324083b/production/ksonnet/loki/config.libsonnet#L18) config to `true`, then there is nothing to do here for this change.
If you are not using Query Scheduler, then to avoid any issues on the Read path until the rollout finishes, it would be good to follow below steps:

- Create just the `query-frontend-headless` service without applying any changes to the `query-frontend` service.
- Rollout changes to `queriers`.
- Roll out the rest of the changes.

### General

#### Store & Cache Statistics

Statistics are now logged in `metrics.go` lines about how long it takes to download chunks from the store, as well as how long it takes to download chunks, index query, and result cache responses from cache.

Example (note the `*_download_time` fields):

```bash
level=info ts=2022-12-20T15:27:54.858554127Z caller=metrics.go:147 component=frontend org_id=docker latency=fast query="sum(count_over_time({job=\"generated-logs\"}[1h]))" query_type=metric range_type=range length=6h17m48.865587821s start_delta=6h17m54.858533178s end_delta=5.99294552s step=1m30s duration=5.990829396s status=200 limit=30 returned_lines=0 throughput=123MB total_bytes=738MB total_entries=1 store_chunks_download_time=2.319297059s queue_time=2m21.476090991s subqueries=8 cache_chunk_req=81143 cache_chunk_hit=32390 cache_chunk_bytes_stored=1874098 cache_chunk_bytes_fetched=94289610 cache_chunk_download_time=56.96914ms cache_index_req=994 cache_index_hit=710 cache_index_download_time=1.587842ms cache_result_req=7 cache_result_hit=0 cache_result_download_time=380.555µs
```

These statistics are also displayed when using `--stats` with LogCLI.

## 2.7.0

### Loki 2.7.0

### Loki Canary Permission

The new `push` mode to [Loki canary](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/loki-canary/) can push logs that are generated by a Loki canary directly to a given Loki URL. Previously, it only wrote to a local file and you needed some agent, such as promtail, to scrape and push it to Loki.
So if you run Loki behind some proxy with different authorization policies to read and write to Loki, then auth credentials we pass to Loki canary now needs to have both `READ` and `WRITE` permissions.

### `engine.timeout` and `querier.query_timeout` are deprecated

Previously, we had two configurations to define a query timeout: `engine.timeout` and `querier.query-timeout`.
As they were conflicting and `engine.timeout` isn't as expressive as `querier.query-timeout`,
we're deprecating it and moving it to [Limits Config](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#limits_config) `limits_config.query_timeout` with same default values.

#### `fifocache` has been renamed

The in-memory `fifocache` has been renamed to `embedded-cache`. This allows us to replace the implementation (currently a simple FIFO data structure) with something else in the future without causing confusion

#### Evenly spread Memcached pods for chunks across kubernetes nodes

We now evenly spread memcached_chunks pods across the available kubernetes nodes, but allowing more than one pod to be scheduled into the same node.
If you want to run at most a single pod per node, set `$.memcached.memcached_chunks.use_topology_spread` to false.

While we attempt to schedule at most 1 memcached_chunks pod per Kubernetes node with the `topology_spread_max_skew: 1` field,
if no more nodes are available then multiple pods will be scheduled on the same node.
This can potentially impact your service's reliability so consider tuning these values according to your risk tolerance.

#### Evenly spread distributors across kubernetes nodes

We now evenly spread distributors across the available kubernetes nodes, but allowing more than one distributors to be scheduled into the same node.
If you want to run at most a single distributors per node, set `$._config.distributors.use_topology_spread` to false.

While we attempt to schedule at most 1 distributor per Kubernetes node with the `topology_spread_max_skew: 1` field,
if no more nodes are available then multiple distributors will be scheduled on the same node.
This can potentially impact your service's reliability so consider tuning these values according to your risk tolerance.

#### Evenly spread queriers across kubernetes nodes

We now evenly spread queriers across the available kubernetes nodes, but allowing more than one querier to be scheduled into the same node.
If you want to run at most a single querier per node, set `$._config.querier.use_topology_spread` to false.

While we attempt to schedule at most 1 querier per Kubernetes node with the `topology_spread_max_skew: 1` field,
if no more nodes are available then multiple queriers will be scheduled on the same node.
This can potentially impact your service's reliability so consider tuning these values according to your risk tolerance.

#### Default value for `server.http-listen-port` changed

This value now defaults to 3100, so the Loki process doesn't require special privileges. Previously, it had been set to port 80, which is a privileged port. If you need Loki to listen on port 80, you can set it back to the previous default using `-server.http-listen-port=80`.

#### docker-compose setup has been updated

The docker-compose [setup](https://github.com/grafana/loki/blob/main/production/docker) has been updated to **v2.6.0** and includes many improvements.

Notable changes include:

- authentication (multi-tenancy) is **enabled** by default; you can disable it in `production/docker/config/loki.yaml` by setting `auth_enabled: false`
- storage is now using Minio instead of local filesystem
  - move your current storage into `.data/minio` and it should work transparently
- log-generator was added - if you don't need it, simply remove the service from `docker-compose.yaml` or don't start the service

#### Configuration for deletes has changed

The global `deletion_mode` option in the compactor configuration moved to runtime configurations.

- The `deletion_mode` option needs to be removed from your compactor configuration
- The `deletion_mode` global override needs to be set to the desired mode: `disabled`, `filter-only`, or `filter-and-delete`. By default, `filter-and-delete` is enabled.
- Any `allow_delete` per-tenant overrides need to be removed or changed to `deletion_mode` overrides with the desired mode.

#### Metric name for `loki_log_messages_total` changed

The name of this metric was changed to `loki_internal_log_messages_total` to reduce ambiguity. The previous name is still present but is deprecated.

#### Usage Report  / Telemetry config has changed named

The configuration for anonymous usage statistics reporting to Grafana has changed from `usage_report` to `analytics`.

#### TLS `cipher_suites` and `tls_min_version` have moved

These were previously configurable under `server.http_tls_config` and `server.grpc_tls_config` separately. They are now under `server.tls_cipher_suites` and `server.tls_min_version`. These values are also now configurable for individual clients, for example: `distributor.ring.etcd` or `querier.ingester_client.grpc_client_config`.

#### `ruler.storage.configdb` has been removed

ConfigDB was disallowed as a Ruler storage option back in 2.0. The config struct has finally been removed.

#### `ruler.remote_write.client` has been removed

Can no longer specify a remote write client for the ruler.

### Promtail 2.7.0

#### `gcp_push_target_parsing_errors_total` has a new `reason` label

The `gcp_push_target_parsing_errors_total` GCP Push Target metrics has been added a new label named `reason`. This includes detail on what might have caused the parsing to fail.

#### Windows event logs: now correctly includes `user_data`

The contents of the `user_data` field was erroneously set to the same value as `event_data` in previous versions. This was fixed in [#7461](https://github.com/grafana/loki/pull/7461) and log queries relying on this broken behavior may be impacted.

## 2.6.0

### Loki 2.6.0

#### Implementation of unwrapped `rate` aggregation changed

The implementation of the `rate()` aggregation function changed back to the previous implementation prior to [#5013](https://github.com/grafana/loki/pulls/5013).
This means that the rate per second is calculated based on the sum of the extracted values, instead of the average increase over time.

If you want the extracted values to be treated as [Counter](https://prometheus.io/docs/concepts/metric_types/#counter) metric, you should use the new `rate_counter()` aggregation function, which calculates the per-second average rate of increase of the vector.

#### Default value for `azure.container-name` changed

This value now defaults to `loki`, it was previously set to `cortex`. If you are relying on this container name for your chunks or ruler storage, you will have to manually specify `-azure.container-name=cortex` or `-ruler.storage.azure.container-name=cortex` respectively.

## 2.5.0

### Loki 2.5.0

#### `split_queries_by_interval` yaml configuration has moved

It was previously possible to define this value in two places

```yaml
query_range:
  split_queries_by_interval: 10m
```

and/or

```yaml
limits_config:
  split_queries_by_interval: 10m
```

In 2.5.0 it can only be defined in the `limits_config` section, **Loki will fail to start if you do not remove the `split_queries_by_interval` config from the `query_range` section.**

Additionally, it has a new default value of `30m` rather than `0`.

The CLI flag is not changed and remains `querier.split-queries-by-interval`.

#### Dropped support for old Prometheus rules configuration format

Alerting rules previously could be specified in two formats: 1.x format (legacy one, named `v0` internally) and 2.x.
We decided to drop support for format `1.x` as it is fairly old and keeping support for it required a lot of code.

In case you're still using the legacy format, take a look at
[Alerting Rules](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/) for instructions
on how to write alerting rules in the new format.

For reference, the newer format follows a structure similar to the one below:

```yaml
 groups:
 - name: example
   rules:
   - alert: HighErrorRate
     expr: job:request_latency_seconds:mean5m{job="myjob"} > 0.5
     for: 10m
     labels:
       severity: page
     annotations:
       summary: High request latency
```

Meanwhile, the legacy format is a string in the following format:

```
 ALERT <alert name>
   IF <expression>
   [ FOR <duration> ]
   [ LABELS <label set> ]
   [ ANNOTATIONS <label set> ]
```

#### Changes to default configuration values

* `parallelize_shardable_queries` under the `query_range` config now defaults to `true`.
* `split_queries_by_interval` under the `limits_config` config now defaults to `30m`, it was `0s`.
* `max_chunk_age` in the `ingester` config now defaults to `2h` previously it was `1h`.
* `query_ingesters_within` under the `querier` config now defaults to `3h`, previously it was `0s`. Any query (or subquery) that has an end time more than `3h` ago will not be sent to the ingesters, this saves work on the ingesters for data they normally don't contain. If you regularly write old data to Loki you may need to return this value to `0s` to always query ingesters.
* `max_concurrent` under the `querier` config now defaults to `10` instead of `20`.
* `match_max_concurrent` under the `frontend_worker` config now defaults to true, this supersedes the `parallelism` setting which can now be removed from your config. Controlling query parallelism of a single process can now be done with the `querier` `max_concurrent` setting.
* `flush_op_timeout` under the `ingester` configuration block now defaults to `10m`, increased from `10s`. This can help when replaying a large WAL on Loki startup, and avoid `msg="failed to flush" ... context deadline exceeded` errors.

### Promtail 2.5.0

#### `gcplog` labels have changed

- Resource labels have been moved from `__<NAME>` to `__gcp_resource_labels_<NAME>` for example, if you previously used `__project_id` then you'll need to update your relabel config to use `__gcp_resource_labels_project_id`.
- `resource_type` has been moved to `__gcp_resource_type`

#### `promtail_log_entries_bytes_bucket` histogram has been removed

This histogram reports the distribution of log line sizes by file. It has 8 buckets for every file being tailed.

This creates a lot of series and we don't think this metric has enough value to offset the amount of series generated so we are removing it.

#### `added Docker target` log message has been demoted from level=error to level=info

If you have dashboards that depended on the log level, change them to search for the `msg="added
Docker target"` property.

### Jsonnet 2.5.0

#### Compactor config defined as command line args moved to yaml config

Following 2 compactor configs that were defined as command line arguments in jsonnet are now moved to yaml config:

```yaml
# Directory where files can be downloaded for compaction.
# CLI flag: -boltdb.shipper.compactor.working-directory
[working_directory: <string>]

# The shared store used for storing boltdb files.
# Supported types: gcs, s3, azure, swift, cos, filesystem.
# CLI flag: -boltdb.shipper.compactor.shared-store
[shared_store: <string>]
```

## 2.4.0

The following are important changes which should be reviewed and understood prior to upgrading Loki.

### Loki 2.4.0

The following changes pertain to upgrading Loki.

#### The single binary no longer runs a table-manager

Single binary Loki means running loki with `-target=all` which is the default if no `-target` flag is passed.

This will impact anyone in the following scenarios:

1. Running a single binary Loki with any index type other than `boltdb-shipper` or `boltdb`
2. Relying on retention with the configs `retention_deletes_enabled` and `retention_period`

Anyone in situation #1 who is not using `boltdb-shipper` or `boltdb` (e.g. `cassandra` or `bigtable`) should modify their Loki command to include `-target=all,table-manager` this will instruct Loki to run a table-manager for you.

Anyone in situation #2, you have two options, the first (and not recommended) is to run Loki with a table-manager by adding `-target=all,table-manager`.

The second and recommended solution, is to use deletes via the compactor:

```yaml
compactor:
  retention_enabled: true
limits_config:
  retention_period: [30d]
```

See the [retention docs](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/) for more info.

#### Log messages on startup: proto: duplicate proto type registered

PR [#3842](https://github.com/grafana/loki/pull/3842) **cyriltovena**: Fork cortex chunk storage into Loki.

Since Cortex doesn't plan to use the `chunk` package anymore, we decided to fork it into our storage package to
be able to evolve and modify it easily. However, as a side-effect, we still vendor Cortex which includes this forked
code and protobuf files resulting in log messages like these at startup:

```bash
2021-11-04 15:30:02.437911 I | proto: duplicate proto type registered: purgeplan.DeletePlan
2021-11-04 15:30:02.437936 I | proto: duplicate proto type registered: purgeplan.ChunksGroup
2021-11-04 15:30:02.437939 I | proto: duplicate proto type registered: purgeplan.ChunkDetails
...
```

The messages are harmless and we will work to remove them in the future.

#### Change of some default limits to common values

PR [4415](https://github.com/grafana/loki/pull/4415) **DylanGuedes**: the default value of some limits were changed to protect users from overwhelming their cluster with ingestion load caused by relying on default configs.

We suggest you double check if the following parameters are
present in your Loki config: `ingestion_rate_strategy`, `max_global_streams_per_user`
`max_query_length` `max_query_parallelism` `max_streams_per_user`
`reject_old_samples` `reject_old_samples_max_age`. If they are not present, we recommend you double check that the new values will not negatively impact your system. The changes are:

| config | new default | old default |
| --- | --- | --- |
| ingestion_rate_strategy | "global" | "local" |
| max_global_streams_per_user | 5000 | 0 (no limit) |
| max_query_length | "721h" | "0h" (no limit) |
| max_query_parallelism | 32 | 14 |
| max_streams_per_user | 0 (no limit) | 10000 |
| reject_old_samples | true | false |
| reject_old_samples_max_age | "168h" | "336h" |
| per_stream_rate_limit | 3MB | - |
| per_stream_rate_limit_burst | 15MB | - |

#### Change of configuration defaults

| config | new default | old default |
| --- | --- | --- |
| chunk_retain_period | 0s | 30s |
| chunk_idle_period | 30m | 1h |
| chunk_target_size | 1572864 | 1048576 |

* chunk_retain_period is necessary when using an index queries cache which is not enabled by default. If you have configured an index_queries_cache_config section make sure that you set chunk_retain_period larger than your cache TTL
* chunk_idle_period is how long before a chunk which receives no logs is flushed.
* chunk_target_size was increased to flush slightly larger chunks, if using memcache for a chunks store make sure it will accept files up to 1.5MB in size.

#### In memory FIFO caches enabled by default

Loki now enables a results cache and chunks cache in memory to improve performance. This can however increase memory usage as the cache's by default are allowed to consume up to 1GB of memory.

If you would like to disable these caches or change this memory limit:

Disable:

```yaml
chunk_store_config:
  chunk_cache_config:
    enable_fifocache: false
query_range:
  results_cache:
    cache:
      enable_fifocache: false
```

Resize:

```yaml
chunk_store_config:
  chunk_cache_config:
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
query_range:
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
```

#### Ingester Lifecycler `final_sleep` now defaults to `0s`

* [4608](https://github.com/grafana/loki/pull/4608) **trevorwhitney**: Change default value of ingester lifecycler's `final_sleep` from `30s` to `0s`

This final sleep exists to keep Loki running for long enough to get one final Prometheus scrape before shutting down, however it also causes Loki to sit idle for 30s on shutdown which is an annoying experience for many people.

We decided the default would be better to disable this sleep behavior but anyone can set this config variable directly to return to the previous behavior.

#### Ingester WAL now defaults to on, and chunk transfers are disabled by default

* [4543](https://github.com/grafana/loki/pull/4543) **trevorwhitney**: Change more default values and improve application of common storage config
* [4629](https://github.com/grafana/loki/pull/4629) **owen-d**: Default the WAL to enabled in the Loki jsonnet library
* [4624](https://github.com/grafana/loki/pull/4624) **chaudum**: Disable chunk transfers in jsonnet lib

This changes a few default values, resulting in the ingester WAL now being on by default,
and chunk transfer retries are disabled by default. Note, this now means Loki will depend on local disk by default for its WAL (write ahead log) directory. This defaults to `wal` but can be overridden via the `--ingester.wal-dir` or via `path_prefix` in the common configuration section. Below are config snippets with the previous defaults, and another with the new values.

Previous defaults:

```yaml
ingester:
  max_transfer_retries: 10
  wal:
    enabled: false
```

New defaults:

```yaml
ingester:
  max_transfer_retries: 0
  wal:
    enabled: true
```

Using the write ahead log (WAL) is recommended and is now the default. However using the WAL is incompatible with chunk transfers, if you have explicitly configured `ingester.max-transfer-retries` to a non-zero value, you must set it to 0 to disable transfers.

#### Memberlist config now automatically applies to all non-configured rings

* [4400](https://github.com/grafana/loki/pull/4400) **trevorwhitney**: Config: automatically apply memberlist config too all rings when provided

This change affects the behavior of the ingester, distributor, and ruler rings. Previously, if you wanted to use memberlist for all of these rings, you
had to provide a `memberlist` configuration as well as specify `store: memberlist` for the `kvstore` of each of the rings you wanted to use memberlist.
For example, your configuration might look something like this:

```yaml
memberlist:
  join_members:
    - loki.namespace.svc.cluster.local
distributor:
  ring:
    kvstore:
      store: memberlist
ingester:
    lifecycler:
      ring:
        kvstore:
          store: memberlist
ruler:
  ring:
    kvstore:
      store: memberlist
```

Now, if your provide a `memberlist` configuration with at least one `join_members`, loki will default all rings to use a `kvstore` of type `memberlist`.
You can change this behavior by overriding specific configurations. For example, if you wanted to use `consul` for you `ruler` rings, but `memberlist`
for the `ingester` and `distributor`, you could do so with the following config (although we don't know why someone would want to do this):

```yaml
memberlist:
  join_members:
    - loki.namespace.svc.cluster.local
ruler:
  ring:
    kvstore:
      store: consul
      consul:
        host: consul.namespace.svc.cluster.local:8500
```

#### Changed defaults for some GRPC server settings

* [4435](https://github.com/grafana/loki/pull/4435) **trevorwhitney**: Change default values for two GRPC settings so querier can connect to frontend/scheduler

This changes two default values, `grpc_server_min_time_between_pings` and `grpc_server_ping_without_stream_allowed` used by the GRPC server.

*Previous Values*:

```yaml
server:
  grpc_server_min_time_between_pings: '5m'
  grpc_server_ping_without_stream_allowed: false
```

*New Values*:

```yaml
server:
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
```

[This issue](https://github.com/grafana/loki/issues/4375) has some more information on the change.

#### Some metric prefixes have changed from `cortex_` to `loki_`

* [#3842](https://github.com/grafana/loki/pull/3842)/[#4253](https://github.com/grafana/loki/pull/4253) **jordanrushing**: Metrics related to chunk storage and runtime config have changed their prefixes from `cortex_` to `loki_`.

```yaml
cortex_runtime_config* -> loki_runtime_config*
cortex_chunks_store* -> loki_chunks_store*
```

#### Recording rules storage is now durable

* [4344](https://github.com/grafana/loki/pull/4344) **dannykopping**: per-tenant WAL

Previously, samples generated by recording rules would only be buffered in memory before being remote-written to Prometheus; from this
version, the `ruler` now writes these samples to a per-tenant Write-Ahead Log for durability. More details about the
per-tenant WAL can be found [here](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/recording-rules/).

The `ruler` now requires persistent storage - see the
[Operations](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/recording-rules/#deployment) page for more details about deployment.

### Promtail 2.4.0

The following changes pertain to upgrading Promtail.

#### Promtail no longer insert `promtail_instance` label when scraping `gcplog` target

* [4556](https://github.com/grafana/loki/pull/4556) **james-callahan**: Remove `promtail_instance` label that was being added by promtail when scraping `gcplog` target.

## 2.3.0

### Loki 2.3.0

#### Query restriction introduced for queries which do not have at least one equality matcher

PR [3216](https://github.com/grafana/loki/pull/3216) **sandeepsukhani**: check for stream selectors to have at least one equality matcher

This change now rejects any query which does not contain at least one equality matcher, an example may better illustrate:

`{namespace=~".*"}`

This query will now be rejected, however there are several ways to modify it for it to succeed:

Add at least one equals label matcher:

`{cluster="us-east-1",namespace=~".*"}`

Use `.+` instead of `.*`

`{namespace=~".+"}`

This difference may seem subtle but if we break it down `.` matches any character, `*` matches zero or more of the preceding character and `+` matches one or more of the preceding character. The `.*` case will match empty values where `.+` will not, this is the important difference. `{namespace=""}` is an invalid request (unless you add another equals label matcher like the example above)

The reasoning for this change has to do with how index lookups work in Loki, if you don't have at least one equality matcher Loki has to perform a complete index table scan which is an expensive and slow operation.

## 2.2.0

### Loki 2.2.0

**Be sure to upgrade to 2.0 or 2.1 BEFORE upgrading to 2.2**

In Loki 2.2 we changed the internal version of our chunk format from v2 to v3, this is a transparent change and is only relevant if you every try to _downgrade_ a Loki installation. We incorporated the code to read v3 chunks in 2.0.1 and 2.1, as well as 2.2 and any future releases.

**If you upgrade to 2.2+ any chunks created can only be read by 2.0.1, 2.1 and 2.2+**

This makes it important to first upgrade to 2.0, 2.0.1, or 2.1 **before** upgrading to 2.2 so that if you need to rollback for any reason you can do so easily.

{{< admonition type="note" >}}
2.0 and 2.0.1 are identical in every aspect except 2.0.1 contains the code necessary to read the v3 chunk format. Therefor if you are on 2.0 and upgrade to 2.2, if you want to rollback, you must rollback to 2.0.1.
{{< /admonition >}}

### Loki Config

**Read this if you use the query-frontend and have `sharded_queries_enabled: true`**

We discovered query scheduling related to sharded queries over long time ranges could lead to unfair work scheduling by one single query in the per tenant work queue.

The `max_query_parallelism` setting is designed to limit how many split and sharded units of 'work' for a single query are allowed to be put into the per tenant work queue at one time. The previous behavior would split the query by time using the `split_queries_by_interval` and compare this value to `max_query_parallelism` when filling the queue, however with sharding enabled, every split was then sharded into 16 additional units of work after the `max_query_parallelism` limit was applied.

In 2.2 we changed this behavior to apply the `max_query_parallelism` after splitting _and_ sharding a query resulting a more fair and expected queue scheduling per query.

**What this means** Loki will be putting much less work into the work queue per query if you are using the query frontend and have sharding_queries_enabled (which you should).  **You may need to increase your `max_query_parallelism` setting if you are noticing slower query performance** In practice, you may not see a difference unless you were running a cluster with a LOT of queriers or queriers with a very high `parallelism` frontend_worker setting.

You could consider multiplying your current `max_query_parallelism` setting by 16 to obtain the previous behavior, though in practice we suspect few people would really want it this high unless you have a significant querier worker pool.

**Also be aware to make sure `max_outstanding_per_tenant` is always greater than `max_query_parallelism` or large queries will automatically fail with a 429 back to the user.**

### Promtail 2.2.0

For 2.0 we eliminated the long deprecated `entry_parser` configuration in Promtail configs, however in doing so we introduced a very confusing and erroneous default behavior:

If you did not specify a `pipeline_stages` entry you would be provided with a default which included the `docker` pipeline stage.  This can lead to some very confusing results.

In [3404](https://github.com/grafana/loki/pull/3404), we corrected this behavior

**If you are using docker, and any of your `scrape_configs` are missing a `pipeline_stages` definition**, you should add the following to obtain the correct behavior:

```yaml
pipeline_stages:
  - docker: {}
```

## 2.1.0

The upgrade from 2.0.0 to 2.1.0 should be fairly smooth, be aware of these two things:

### Helm charts have moved

Helm charts are now located in the [Grafana Helm charts repo](https://github.com/grafana/helm-charts/).

The helm repo URL is now: [https://grafana.github.io/helm-charts](https://grafana.github.io/helm-charts).

### Fluent Bit plugin renamed

Fluent bit officially supports Loki as an output plugin now.

However this created a naming conflict with our existing output plugin (the new native output uses the name `loki`) so we have renamed our plugin.

In time our plan is to deprecate and eliminate our output plugin in favor of the native Loki support. However until then you can continue using the plugin with the following change:

Old:

```shell
[Output]
    Name loki
```

New:

```shell
[Output]
    Name grafana-loki
```

## 2.0.0

This is a major Loki release and there are some very important upgrade considerations.
For the most part, there are very few impactful changes and for most this will be a seamless upgrade.

2.0.0 Upgrade Topics:

* [IMPORTANT If you are using a docker image, read this!](#important-if-you-are-using-a-docker-image-read-this)
* [IMPORTANT boltdb-shipper upgrade considerations](#important-boltdb-shipper-upgrade-considerations)
* [IMPORTANT results_cachemax_freshness removed from yaml config](#important-results_cachemax_freshness-removed-from-yaml-config)
* [Promtail removed entry_parser config](#promtail-config-removed)
* [If you would like to use the new single store index and v11 schema](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema)

### **IMPORTANT If you are using a docker image, read this!**

(This includes, Helm, Tanka, docker-compose etc.)

The default config file in the docker image, as well as the default helm values.yaml and jsonnet for Tanka all specify a schema definition to make things easier to get started.

{{< admonition type="caution" >}}
If you have not specified your own config file with your own schema definition (or you do not have a custom schema definition in your values.yaml), upgrading to 2.0 will break things!
{{< /admonition >}}

In 2.0 the defaults are now v11 schema and the `boltdb-shipper` index type.

If you are using an index type of `aws`, `bigtable`, or `cassandra` this means you have already defined a custom schema and there is _nothing_ further you need to do regarding the schema.
You can consider however adding a new schema entry to use the new `boltdb-shipper` type if you want to move away from these separate index stores and instead use just one object store.

#### What to do

The minimum action required is to create a config which specifies the schema to match what the previous defaults were.

(Keep in mind this will only tell Loki to use the old schema default, if you would like to upgrade to v11 and/or move to the single store boltdb-shipper, [see below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema))

There are three places we have hard coded the schema definition:

##### Helm

Helm has shipped with the same internal schema in the values.yaml file for a very long time.

If you are providing your own values.yaml file then there is no _required_ action because you already have a schema definition.

**If you are not providing your own values.yaml file, you will need to make one!**

We suggest using the included [values.yaml file from the 1.6.0 tag](https://raw.githubusercontent.com/grafana/loki/v1.6.0/production/helm/loki/values.yaml)

This matches what the default values.yaml file had prior to 2.0 and is necessary for Loki to work post 2.0

As mentioned above, you should also consider looking at moving to the v11 schema and boltdb-shipper [see below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema) for more information.

##### Tanka

This likely only affects a small portion of tanka users because the default schema config for Loki was forcing `GCS` and `bigtable`.

**If your `main.jsonnet` (or somewhere in your manually created jsonnet) does not have a schema config section then you will need to add one like this!**

```jsonnet
{
  _config+:: {
    using_boltdb_shipper: false,
    loki+: {
      schema_config+: {
        configs: [{
          from: '2018-04-15',
          store: 'bigtable',
          object_store: 'gcs',
          schema: 'v11',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '168h',
          },
        }],
      },
    },
  }
}
```

{{< admonition type="note" >}}
If you had set `index_period_hours` to a value other than 168h (the previous default) you must update this in the above config `period:` to match what you chose.
{{< /admonition >}}

{{< admonition type="note" >}}
We have changed the default index store to `boltdb-shipper` it's important to add `using_boltdb_shipper: false,` until you are ready to change (if you want to change)
{{< /admonition >}}

Changing the jsonnet config to use the `boltdb-shipper` type is the same as [below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema) where you need to add a new schema section.

**HOWEVER** Be aware when you change `using_boltdb_shipper: true` the deployment type for the ingesters and queriers will change to StatefulSets! StatefulSets are required for the ingester and querier using boltdb-shipper.

##### Docker (e.g. docker-compose)

For docker related cases you will have to mount a Loki config file separate from what's shipped inside the container

I would recommend taking the previous default file from the [1.6.0 tag on github](https://raw.githubusercontent.com/grafana/loki/v1.6.0/cmd/loki/loki-docker-config.yaml)

How you get this mounted and in use by Loki might vary based on how you are using the image, but this is a common example:

```shell
docker run -d --name=loki --mount type=bind,source="path to loki-config.yaml",target=/etc/loki/local-config.yaml
```

The Loki docker image is expecting to find the config file at `/etc/loki/local-config.yaml`

### IMPORTANT: boltdb-shipper upgrade considerations

Significant changes have taken place between 1.6.0 and 2.0.0 for boltdb-shipper index type, if you are already running this index and are upgrading some extra caution is warranted.

Take a complete backup of the `index` directory in your object store, this location might be slightly different depending on what store you use.
It should be a folder named index with a bunch of folders inside with names like `index_18561`,`index_18560`...

The chunks directory should not need any special backups.

If you have an environment to test this in, do so before upgrading against critical data.

There are 2 significant changes warranting the backup of this data because they will make rolling back impossible:

* A compactor is included which will take existing index files and compact them to one per day and remove non compacted files
* All index files are now gzipped before uploading

The second part is important because 1.6.0 does not understand how to read the gzipped files, so any new files uploaded or any files compacted become unreadable to 1.6.0 or earlier.

_THIS BEING SAID_ we are not expecting problems, our testing so far has not uncovered any problems, but some extra precaution might save data loss in unforeseen circumstances!

Report any problems via GitHub issues or reach us on the #loki slack channel.

{{< admonition type="note" >}}
If are using boltdb-shipper and were running with high availability and separate filesystems, this was a poorly documented and even more experimental mode we toyed with using boltdb-shipper. For now we removed the documentation and also any kind of support for this mode.
{{< /admonition >}}

To use boltdb-shipper in 2.0 you need a shared storage (S3, GCS, etc), the mode of running with separate filesystem stores in HA using a ring is not officially supported.

We didn't do anything explicitly to limit this functionality however we have not had any time to actually test this which is why we removed the docs and are listing it as not supported.

#### If running in microservices, deploy ingesters before queriers

Ingesters now expose a new RPC method that queriers use when the index type is `boltdb-shipper`.
Queriers generally roll out faster than ingesters, so if new queriers query older ingesters using the new RPC, the queries would fail.
To avoid any query downtime during the upgrade, rollout ingesters before queriers.

#### If running the compactor, ensure it has delete permissions for the object storage

The compactor is an optional but suggested component that combines and deduplicates the boltdb-shipper index files. When compacting index files, the compactor writes a new file and deletes unoptimized files. Ensure that the compactor has appropriate permissions for deleting files, for example, s3:DeleteObject permission for AWS S3.

### IMPORTANT: `results_cache.max_freshness` removed from YAML config

The `max_freshness` config from `results_cache` has been removed in favour of another flag called `max_cache_freshness_per_query` in `limits_config` which has the same effect.
If you happen to have `results_cache.max_freshness` set, use `limits_config.max_cache_freshness_per_query` YAML config instead.

### Promtail config removed

The long deprecated `entry_parser` config in Promtail has been removed.

### Upgrading schema to use boltdb-shipper and/or v11 schema

If you would also like to take advantage of the new Single Store (boltdb-shipper) index, as well as the v11 schema if you aren't already using it.

You can do this by adding a new schema entry.

Here is an example:

```yaml
schema_config:
  configs:
    - from: 2018-04-15           ①
      store: boltdb              ①④
      object_store: filesystem   ①④
      schema: v11                ②
      index:
        prefix: index_           ①
        period: 168h             ①
    - from: 2020-10-24           ③
      store: boltdb-shipper
      object_store: filesystem   ④
      schema: v11
      index:
        prefix: index_
        period: 24h              ⑤
```

① Make sure all of these match your current schema config
② Make sure this matches your previous schema version, Helm for example is likely v9
③ Make sure this is a date in the **FUTURE** keep in mind Loki only knows UTC so make sure it's a future UTC date
④ Make sure this matches your existing config (e.g. maybe you were using gcs for your object_store)
⑤ 24h is required for boltdb-shipper

There are more examples on the [Storage description page](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#examples) including the information you need to setup the `storage` section for boltdb-shipper.
