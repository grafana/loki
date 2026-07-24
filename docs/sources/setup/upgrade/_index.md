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

### TSDB schema v14

Loki now supports the experimental TSDB storage schema `v14`. Schema v14 uses the
same chunk format as v13 and changes only the TSDB index format, which adds a
per-chunk ingestion timestamp used by ingestion-time retention features.

v13 remains the recommended schema. To opt in to v14, add a new `period_config`
with a future `from` date and `schema: v14`; existing v13 periods are unaffected.

Before configuring any v14 period, upgrade all components to a version that can
read the v14 index format. Rolling back after v14 data has been written requires
stopping new v14 writes first, because earlier binaries cannot read v14 indexes.

### Breaking change: Thanos storage clients are used by default

The default value of `storage_config.use_thanos_objstore` changed from `false` to `true`, enabling the Thanos based object store clients by default if not otherwise explicitly specified.

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

- The settings `-limits.per-user-override-config` (`limits_config.per_tenant_override_config`) and `-limits.per-user-override-period` (`limits_config.per_tenant_override_period`) have been removed in favor of `-runtime-config.file` (`runtime_config.file`) and `-runtime-config.reload-period` (`runtime_config.period`) respectively.
- The per-tenant setting `unordered_writes` has been removed. Loki now always allows unordered writes.
- The setting `-store.index-cache-write` (`chunk_store_config.write_dedupe_cache_config` block in the yaml file) has been removed as it was only used for legacy storage backends that have been removed as well.
- The setting `-store.index-cache-read` (`storage_config.index_queries_cache_config` block in the yaml file) has been removed as it was only used for legacy storage backends (`boltdb-shipper`) that have been removed as well.
- The setting `-store.index-cache-validity` (`storage_config.index_cache_validity` block in the yaml file) has been removed as it was only used in combination with the removed `-store.index-cache-read` setting.

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

The next Loki release introduces a new configuration option (i.e. `-distibutor.max-recv-msg-size`) for the distributors to control the max receive size of uncompressed stream data. The new options's default value is set to `100MB`.

Supported clients should check the configuration options for max send message size if applicable.

### Ruler WAL directory now honors `common.path_prefix`

The default `-ruler.wal.dir` (`ruler-wal`) is now composed against `common.path_prefix` when one is set, matching the behavior of the other path-bearing defaults already rewritten by Loki: `-ruler.rule-path`, `-ingester.wal-dir`, `-compactor.working-directory`, and `-bloom.shipper.working-directory`. Previously the ruler WAL stayed at the cwd-relative `ruler-wal`, which fails on `mkdir` in read-only-rootfs containers (#7377).

Deployments that set `common.path_prefix` but did not explicitly set `-ruler.wal.dir` will see the ruler create a new WAL directory at `<path_prefix>/ruler-wal` after the upgrade. The previous cwd-relative `ruler-wal` directory is not migrated; any un-remote-written recording-rule samples buffered there are dropped on first restart.

To preserve the previous location, set `-ruler.wal.dir` explicitly to the old path (e.g. `ruler-wal` or its absolute equivalent) in your config before upgrading. Deployments that already set `-ruler.wal.dir` explicitly are unaffected.

## Helm Chart Upgrades

{{< admonition type="note" >}}
With the move to the [Grafana-community/helm-charts repository](https://github.com/grafana-community/helm-charts), the chart numbering has changed. Major version updates signal breaking changes in the chart. For more information, refer to the [README](https://github.com/grafana-community/helm-charts/blob/main/charts/loki/README.md#upgrading).
{{< /admonition >}}

### Migrating from the Loki Repository Helm Chart to the Community Helm Chart

If you are upgrading from the Helm chart previously hosted in the Loki repository (chart version 6.x) to the Grafana Community Helm chart (chart version 18.x), refer to the dedicated migration guide: [Upgrade to the Community Helm chart](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-to-community/).

### Helm Chart 6.50.0 - Respect the global registry in the sidecar image

If you prefixed the sidecar container with a private registry (`sidecar.image.repository`), this is no longer necessary and is deprecated as the global registry is used starting with Helm chart 6.46.1. Therefore please use `global.imageRegistry` or alternatively, `sidecar.image.registry` for more fine-grained control.

### Helm Chart 6.50.0 - Uniform naming for image digest also in the sidecar image

For most images used in the helm chart, a `.digest` is available to pin an image to a specific hash. The sidecar images diverges from this convention by introducing a `.tag`.
Starting with Helm chart 6.46.1, the `.tag` is deprecated and `.digest` should be used.

### Helm Chart 6.46.0 - Default service account name change

{{< admonition type="warning" >}}
Helm chart version 6.46.0 introduces a **breaking change** that affects users who rely on the default service account name to bind external identity, such as AWS EKS Pod Identity, IAM Roles for Service Accounts (IRSA), GCP Workload Identity, or Azure Workload Identity.
{{< /admonition >}}

Starting with Helm chart 6.46.0 ([#19590](https://github.com/grafana/loki/pull/19590)), when `serviceAccount.create` is `true` and `serviceAccount.name` is not set, the default service account name is now derived from the chart's *fullname* template instead of the chart *name*. For example:

| Deployment | Default service account name before 6.46.0 | Default service account name in 6.46.0 and later |
|---|---|---|
| Grafana Enterprise Logs (GEL) | `enterprise-logs` | `<release-name>-enterprise-logs` |
| Open source Loki | `loki` | `<release-name>-loki` |

If an external identity, such as an AWS IAM role through EKS Pod Identity or IRSA, is bound to the previous service account name, your pods lose access to that identity after the upgrade. For object-storage backends, this typically results in an outage because Loki components can no longer read or write to the bucket.

**Recommended action**:

To preserve the previous behavior and avoid the rename, set the service account name explicitly in your `values.yaml`:

```yaml
serviceAccount:
  name: enterprise-logs   # use "loki" for open source deployments
```

Setting `serviceAccount.name` explicitly is forward-compatible and works on both pre- and post-6.46.0 chart versions, so it is also the recommended setting going forward if you want the service account name to be independent of the Helm release name.

If you have already upgraded and your pods have lost cloud-provider IAM access, you have two options:
- Set `serviceAccount.name` to the previous default (for example, `enterprise-logs`) and run `helm upgrade` again. The previously bound external identity will resume working.
- Update the external identity binding (for example, the EKS Pod Identity association or the IRSA trust policy) to reference the new service account name.

### Helm Chart 6.34.0 - Zone-aware Ingester Breaking Change

{{< admonition type="warning" >}}
Helm chart version 6.34.0 introduces a **breaking change** that affects users with zone-aware ingester replication enabled.
{{< /admonition >}}

If you are using zone-aware ingesters (`ingester.zoneAwareReplication.enabled: true`), upgrading to Helm chart 6.34.0 requires manual StatefulSet deletion before the upgrade. This is due to a fix for the `serviceName` field in zone-aware ingester StatefulSets, which is an immutable field in Kubernetes.

**For detailed upgrade instructions, see**: [Helm Chart 6.x Upgrade Guide - Zone-aware Ingester Breaking Change](https://grafana.com/docs/loki/latest/setup/upgrade/upgrade-to-6x/#breaking-zone-aware-ingester-statefulset-servicename-fix-6340)

Key points:
- Only affects deployments with `ingester.zoneAwareReplication.enabled: true`
- Requires manual StatefulSet deletion with `--cascade=orphan`
- **No data loss** - PersistentVolumeClaims and data are preserved
- New StatefulSets will be created with correct service references

## 3.6.0

### Loki 3.6.0

#### Upgraded AWS SDK to v2

Loki uses the official AWS SDK for configuring and communication with S3 object storage. Version 1 of the SDK reached its end of life on 31st, 2025, and therefore had to be replaced with Version 2. While the user-facing configuration in Loki did not change, internal functionality of the object store client did change, without affecting functionality of Loki.

Please refer to the full release notes of v2 [https://github.com/aws/aws-sdk-go-v2/releases/tag/release-2025-01-15](https://github.com/aws/aws-sdk-go-v2/releases/tag/release-2025-01-15) for further information and whether you may be impacted by any of the changes. 

## 3.5.0

### Loki 3.5.8

#### Removal of BusyBox Shell from Docker Images

Starting in Loki version **3.5.8**, the `busybox` utility was removed from the official Loki Docker images. This means that **shell utilities like `/bin/sh` are no longer available** inside the container by default.

#### Impact

- **You cannot `exec` into the Loki container to use a shell as before**. Commands like `kubectl exec -it podname -- sh` or `docker exec -it containername sh` will fail because `/bin/sh` does not exist in the image.
- Common utilities provided by BusyBox (e.g., `ls`, `cat`, `ps`) are also not available inside the container.

#### Why was BusyBox removed?

Removing BusyBox addresses the following CVEs:

- CVE-2023-42364
- CVE-2023-42365
- CVE-2023-42363
- CVE-2023-42366
- CVE-2025-46394
- CVE-2024-58251

#### How do I troubleshoot or inspect a running Loki container now?

If you need to debug or inspect a Loki container:
1. **Use ephemeral containers:** Kubernetes allows you to attach an ephemeral container _with a shell_ to a running Pod for debugging (if your cluster supports it).
   - Example:
     ```
     kubectl debug -it <pod-name> --image=busybox --target=<container-name>
     ```
2. **Copy files out/in instead of shelling in:** Use `kubectl cp` or `docker cp` to move logs or config files for inspection.
3. **Include shell utilities in your own derived image:** If your operational process requires a shell, you can build a custom Docker image based on Loki and add BusyBox or another shell to it (not recommended for production).
   - Example Dockerfile snippet:
     ```
     FROM grafana/loki:3.5.8
     USER root
     RUN apk add --no-cache busybox
     USER 10001
     ```

## 3.4.0

### Loki 3.4.0

#### New Object Storage Clients

Loki release 3.4.0 introduces new object storage clients based on the [Thanos Object Storage Client Go module](https://github.com/thanos-io/objstore), this is an opt-in feature.
In a future release, this will become the default way of configuring storage and the existing storage clients will be deprecated.

The new storage configuration deviates from the existing format. Refer to the [Thanos storage configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) to view the complete list of supported storage providers and their configuration options.

The documentation now also includes a [migration guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-storage-clients/) and [configuration examples](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/examples/thanos-storage-configs/) for using Thanos-based storage clients.

## 3.3.0

### Loki 3.3.0

#### Experimental Bloom Filters

With Loki 3.3.0, the bloom block format changed and any previously created block is incompatible with the new format.
Before upgrading, we recommend deleting all the existing bloom blocks in the object store. We store bloom blocks and
metas inside the `bloom` path in the configured object store. To get rid of all the bloom blocks, delete all the objects
inside the `bloom` path in the object store.

## 3.2.0

### Loki 3.2.0

### HTTP API

The API endpoint for instant queries `/api/v1/query` now returns a HTTP status 400 (Bad Request) when the provided `query`
parameter contains a log selector query instead of returning inconsistent results. Please use the range query endpoint
`/api/v1/query_range` (`Range` type in Grafana Explore) instead.

### Configuration

Loki changes the default value of `-ruler.alertmanager-use-v2` from `false` to `true`. Alertmanager APIv1 was deprecated in Alertmanager 0.16.0 and is removed as of 0.27.0.

### Experimental Bloom Filters

{{< admonition type="note" >}}
Experimental features are subject to rapid change and/or removal, which can introduce breaking changes even between minor version.
They also don't follow the deprecation lifecycle of regular features.
{{< /admonition >}}

The bloom compactor component, which builds bloom filter blocks for query acceleration, has been removed in favor of two new components: bloom planner and bloom builder.
Please consult the [Query Acceleration with Blooms](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/query-acceleration-blooms/) docs for more information.

CLI arguments (and their YAML counterparts) of per-tenant settings that have been removed as part of this change:

* `-bloom-compactor.enable-compaction`
* `-bloom-compactor.shard-size`
* `-bloom-compactor.shard-size`
* `-bloom-compactor.shard-size`

CLI arguments of per-tenant settings that have been moved to a different prefix as part of this change:

* `-bloom-compactor.max-page-size` changed to `-bloom-builder.max-page-size`
* `-bloom-compactor.max-block-size` changed to `-bloom-builder.max-block-size`
* `-bloom-compactor.ngram-length` changed to `-bloom-builder.ngram-length`
* `-bloom-compactor.ngram-skip` changed to `-bloom-builder.ngram-skip`
* `-bloom-compactor.false-positive-rate` changed to `-bloom-builder.false-positive-rate`
* `-bloom-compactor.block-encoding` changed to `-bloom-builder.block-encoding`

Their YAML counterparts in the `limits_config` block are kept identical.

All other CLI arguments (and their YAML counterparts) prefixed with `-bloom-compactor.` have been removed.


## 3.0.0

{{< admonition type="note" >}}
If you have questions about upgrading to Loki 3.0, please join us on the [community Slack](https://slack.grafana.com/) in the `#loki-3` channel.

Or leave a comment on this [Github Issue](https://github.com/grafana/loki/issues/12506).
{{< /admonition >}}

{{< admonition type="tip" >}}
If you have not yet [migrated to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/), do so before you upgrade to Loki 3.0.
{{< /admonition >}}

Loki 3.0 is a major version increase and comes with several breaking changes.

Here is the shortlist of things we think most people may encounter:

* Structured metadata is enabled by default and requires `tsdb` and `v13` schema or Loki won't start. Refer to [Structured Metadata, Open Telemetry, Schemas and Indexes](#structured-metadata-open-telemetry-schemas-and-indexes).
* The `shared_store` config is removed. Refer to [Removed `shared_store` and `shared_store_key_prefix` from shipper configuration](#removed-shared_store-and-shared_store_key_prefix-from-shipper-configuration).
* Loki now enforces a max line size of 256KB by default (you can disable this or increase this but this is how Grafana Labs runs Loki). Refer to [Changes to default configure values](#changes-to-default-configuration-values-in-30).
* Loki now enforces a max label limit of 15 labels per series, down from 30. Extra labels inflate the size of the index and reduce performance, you should almost never need more than 15 labels. Refer to [Changes to default configure values](#changes-to-default-configuration-values-in-30).
* Loki will automatically attempt to populate a `service_name` label on ingestion. Refer to [`service_name` label](#service_name-label).
* There are many metric name changes. Refer to [Distributor metric changes](#distributor-metric-changes), [Embedded cache metric changes](#embedded-cache-metric-changes), and [Metrics namespace](#metrics-namespace).

If you would like to see if your existing configuration will work with Loki 3.0:

1. In an empty directory on your computer, copy you configuration into a file named `loki-config.yaml`.
1. Run this command from that directory:

```bash
docker run --rm -t -v "${PWD}":/config grafana/loki:3.0.0 -config.file=/config/loki-config.yaml -verify-config=true
```

{{< admonition type="note" >}}
If you introduce a new schema_config entry it may cause additional validation errors.
{{< /admonition >}}

{{< admonition type="tip" >}}
If you configure `path_prefix` in the `common` config section this can help save a lot of configuration. Refer to the [Common Config Docs](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common).
{{< /admonition >}}

The **Helm chart** has gone through some significant changes and has a separate upgrade guide: [Upgrading to Helm 6.x](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-to-6x/).

### Loki 3.0.0

{{< admonition type="caution" >}}
Before upgrading your software from Loki 2.x to 3.0, you should follow the instructions to [Migrate to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/). 
{{< /admonition >}}

#### Structured Metadata, Open Telemetry, Schemas and Indexes

A flagship feature of Loki 3.0 is native support for the Open Telemetry Protocol (OTLP). This is made possible by a new feature in Loki called [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/), a place for metadata which doesn't belong in labels or log lines. OTel resources and attributes are often a great example of data which doesn't belong in the index nor in the log line.

Structured Metadata is enabled by default in Loki 3.0, however, it requires your active schema be using both the `tsdb` index type AND the `v13` storage schema.  If you are not using both of these you have two options:

* Upgrade your index version and schema version before updating to 3.0, see [schema config upgrade](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/schema/).
* Disable Structured Metadata (and therefore OTLP support) and upgrade to 3.0 and perform the schema migration after. This can be done by setting `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false`.

#### `service_name` label

Loki 3.0 will automatically assign a `service_name` label to all ingested logs by default. A service name is something required by Open Telemetry semantic conventions and is something Grafana Labs is building into our future user interface and query experiences.

Loki will attempt to create the `service_name` label by looking for the following labels in this order:

- service_name
- service
- app
- application
- name
- app_kubernetes_io_name
- container
- container_name
- component
- workload
- job

If no label is found matching the list, a value of `unknown_service` is applied.

You can change this list by providing a list of labels to `discover_service_name` in the [limits_config](/docs/loki/<LOKI_VERSION>/configure/#limits_config) block.

{{< admonition type="note" >}}
If you are already using a `service_label`, Loki will not make a new assignment.
{{< /admonition >}}

**You can disable this by providing an empty value for `discover_service_name`.**

#### Removed `shared_store` and `shared_store_key_prefix` from shipper configuration

The following CLI flags and the corresponding YAML settings to configure shared store for TSDB and BoltDB shippers are now removed:

- `-boltdb.shipper.shared-store`
- `-tsdb.shipper.shared-store`

Going forward the `object_store` setting in the [period_config](/docs/loki/<LOKI_VERSION>/configure/#period_config) will be used to configure the store for the index.
This enforces chunks and index files to reside together in the same storage bucket for a given period.

We are removing the shared store setting in an effort to simplify storage configuration and reduce the possibility for misconfiguration.

{{< admonition type="warning" >}}
With this change Loki no longer allows storing chunks and indexes for a given period in different storage buckets.
This is a breaking change for setups that store chunks and indexes in different storage buckets by setting `-boltdb.shipper.shared-store` or `-tsdb.shipper.shared-store` to a value different from `object_store` in `period_config`.
{{< /admonition >}}

- If you have not configured `-boltdb.shipper.shared-store`,`-tsdb.shipper.shared-store` or their corresponding YAML setting before, no changes are required as part of the upgrade.
- If you have configured `-boltdb.shipper.shared-store` or its YAML setting:
  - If it matches the value of `object_store` for all the periods that use `boltdb-shipper` as index type, no additional changes are required besides removing the usage of the deleted configuration option.
  - If there is a mismatch, you lose access to the index for periods where `-boltdb.shipper.shared-store` does not match `object_store`.
    - To make these indexes queryable, index tables need to moved or copied to the store configured in `object_store`.
- If you have configured `-tsdb.shipper.shared-store` or its YAML setting:
  - If it matches the value of `object_store` for all the periods that use `tsdb` as index type, no additional changes are required besides removing the usage of the deleted configuration option.
  - If there is a mismatch, you lose access to the index for periods where `-tsdb.shipper.shared-store` does not match `object_store`.
    - To make these indexes queryable, index tables need to moved or copied to the store configured in `object_store`.

The following CLI flags and the corresponding YAML settings to configure a path prefix for TSDB and BoltDB shippers are now removed:

- `-boltdb.shipper.shared-store.key-prefix`
- `-tsdb.shipper.shared-store.key-prefix`

Path prefix for storing the index can now be configured by setting `path_prefix` under `index` key in [period_config](/docs/loki/<LOKI_VERSION>/configure/#period_config).
This enables users to change the path prefix by adding a new period config.

```yaml
period_config:
  index:
    path_prefix: "index/"
    period: 24h
```

{{< admonition type="note" >}}
`path_prefix` only applies to TSDB and BoltDB indexes. This setting has no effect on [legacy indexes](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#index-storage).
{{< /admonition >}}

`path_prefix` defaults to `index/` which is same as the default value of the removed configurations.

- No changes are required if you have not configured `-boltdb.shipper.shared-store.key-prefix`, `-tsdb.shipper.shared-store.key-prefix` or the corresponding YAML setting previously.
- If you have configured `-boltdb.shipper.shared-store.key-prefix` or its YAML setting to a value other than `index/`, ensure that all the existing period configs that use `boltdb-shipper` as the index have `path_prefix` set to the value previously configured.
- If you have configured `-tsdb.shipper.shared-store.key-prefix` or its YAML setting to a value other than `index/`, ensure that all the existing period configs that use `tsdb` as the index have the `path_prefix` set to the value previously configured.

#### Removed `shared_store` and `shared_store_key_prefix` from compactor configuration

The following CLI flags and the corresponding YAML settings to configure the shared store and path prefix for compactor are now removed:

- `-boltdb.shipper.compactor.shared-store`
- `-boltdb.shipper.compactor.shared-store.key-prefix`

Going forward compactor will run compaction and retention on all the object stores configured in [period configs](/docs/loki/<LOKI_VERSION>/configure/#period_config) where the index type is either `tsdb` or `boltdb-shipper`.

#### `delete_request_store` should be explicitly configured

`-compactor.delete-request-store` or its YAML setting should be explicitly configured when retention is enabled, this is required for storing delete requests.
The path prefix under which the delete requests are stored is decided by `-compactor.delete-request-store.key-prefix`, it defaults to `index/`.

#### Configuration `async_cache_write_back_concurrency` and `async_cache_write_back_buffer_size` have been removed

These configurations were redundant with the `Background` configuration in the [cache-config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#cache_config).

`async_cache_write_back_concurrency` can be set with `writeback_goroutines`
`async_cache_write_back_buffer_size` can be set with `writeback_buffer`

additionally the `Background` configuration also lest you set `writeback_size_limit` which can be used to set a maximum amount of memory to use for writeback objects vs a count of objects.

#### Legacy ingester shutdown handler is removed

The already deprecated handler `/ingester/flush_shutdown` is removed in favor of `/ingester/shutdown?flush=true`.

#### Ingester configuration `max_transfer_retries` is removed

The setting `max_transfer_retries` (`-ingester.max-transfer-retries`) is removed in favor of the Write Ahead log (WAL).
It was used to allow transferring chunks to new ingesters when the old ingester was shutting down during a rolling restart.
Alternatives to this setting are:

- **A. (Preferred)** Enable the WAL and rely on the new ingester to replay the WAL.
  - Optionally, you can enable `flush_on_shutdown` (`-ingester.flush-on-shutdown`) to flush to long-term storage on shutdowns.
- **B.** Manually flush during shutdowns via [the ingester `/shutdown?flush=true` endpoint](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/api/#flush-in-memory-chunks-and-shut-down).

#### Removed the `default` section of the runtime overrides config file

This was introduced in 2.9 and likely not widely used.  This only affects you if you run Loki with a runtime config file AND you had populated the new `default` block added in 2.9.

The `default` block was removed and instead a top level config now exists in the standard Loki config called `operational_config`, you can set default values here for runtime configs.

#### Configuration `use_boltdb_shipper_as_backup` is removed

The setting `use_boltdb_shipper_as_backup` (`-tsdb.shipper.use-boltdb-shipper-as-backup`) was a remnant from the development of the TSDB storage.
It was used to allow writing to both TSDB and BoltDB when TSDB was still highly experimental.
Since TSDB is now stable and the recommended index type, the setting has become irrelevant and therefore was removed.
The previous default value `false` is applied.

#### Deprecated configuration options are removed

1. Removed already deprecated `store.max-look-back-period` CLI flag and the corresponding YAML settings. Use `querier.max-query-lookback` config instead.
1. Removes already deprecated `-querier.engine.timeout` CLI flag and the corresponding YAML setting.
1. Also removes the `query_timeout` from the querier YAML section. Instead of configuring `query_timeout` under `querier`, you now configure it in [Limits Config](/docs/loki/<LOKI_VERSION>/configuration/#limits_config).
1. `s3.sse-encryption` is removed. AWS now defaults encryption of all buckets to SSE-S3. Use `sse.type` to set SSE type.
1. `ruler.wal-cleaer.period` is removed. Use `ruler.wal-cleaner.period` instead.
1. `experimental.ruler.enable-api` is removed. Use `ruler.enable-api` instead.
1. `split_queries_by_interval` is removed from `query_range` YAML section. You can instead configure it in [Limits Config](/docs/loki/<LOKI_VERSION>/configuration/#limits_config).
1. `frontend.forward-headers-list` CLI flag and its corresponding YAML setting are removed.
1. `frontend.cache-split-interval` CLI flag is removed. Results caching interval is now determined by `querier.split-queries-by-interval`.
1. `querier.worker-parallelism` CLI flag and its corresponding yaml setting are now removed as it does not offer additional value to already existing `querier.max-concurrent`.
    We recommend configuring `querier.max-concurrent` to limit the max concurrent requests processed by the queriers.
1. `ruler.evaluation-delay-duration` CLI flag and the corresponding YAML setting are removed.
1. `validation.enforce-metric-name` CLI flag and the corresponding YAML setting are removed.
1. `boltdb.shipper.compactor.deletion-mode` CLI flag and the corresponding YAML setting are removed. You can instead configure the `compactor.deletion-mode` CLI flag or `deletion_mode` YAML setting in [Limits Config](/docs/loki/<LOKI_VERSION>/configuration/#limits_config).
1. Compactor CLI flags that use the prefix `boltdb.shipper.compactor.` are removed. You can instead use CLI flags with the `compactor.` prefix.

#### Distributor metric changes

The `loki_distributor_ingester_append_failures_total` metric has been removed in favour of `loki_distributor_ingester_append_timeouts_total`.
This new metric will provide a more clear signal that there is an issue with ingesters, and this metric can be used for high-signal alerting.

#### Changes to default configuration values in 3.0

{{< responsive-table >}}

| configuration                                          | new default | old default | notes |
| ------------------------------------------------------ | ----------- | ----------- | --------|
| `compactor.delete-max-interval`                        | 24h         | 0           | splits the delete requests into intervals no longer than `delete_max_interval` |
| `distributor.max-line-size`                            | 256KB       | 0           | - |
| `ingester.sync-period`                                 | 1h          | 0           | ensures that the chunk cuts for a given stream are synchronized across the ingesters in the replication set. Helps with deduplicating chunks. |
| `ingester.sync-min-utilization`                        | 0.1         | 0           | - |
| `frontend.max-querier-bytes-read`                      | 150GB       | 0           | - |
| `frontend.max-cache-freshness`                         | 10m         | 1m          | - |
| `frontend.max-stats-cache-freshness`                   | 10m         | 0           | - |
| `frontend.embedded-cache.max-size-mb`                  | 100MB       | 1GB         | embedded results cache size now defaults to 100MB |
| `memcached.batchsize`                                  | 4           | 1024        | - |
| `memcached.parallelism`                                | 5           | 100         | - |
| `querier.compress-http-responses`                      | true        | false       | compress response if the request accepts gzip encoding |
| `querier.max-concurrent`                               | 4           | 10          | Consider increasing this if queriers have access to more CPU resources. Note that you risk running into out of memory errors if you set this to a very high value. |
| `querier.split-queries-by-interval`                    | 1h          | 30m         | - |
| `querier.tsdb-max-query-parallelism`                   | 128         | 512         | - |
| `query-scheduler.max-outstanding-requests-per-tenant`  | 32000       | 100         | - |
| `validation.max-label-names-per-series`                | 15          | 30          | - |
| `legacy-read-mode`                                     | false       | true        | Deprecated. It will be removed in the next minor release. |

{{< /responsive-table >}}

#### Automatic stream sharding is enabled by default

Automatic stream sharding helps keep the write load of high volume streams balanced across ingesters and helps to avoid hot-spotting. Check out the [operations page](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/automatic-stream-sharding/) for more information

#### More results caching is enabled by default

The TSDB index type has support for caching results for 'stats' and 'volume' queries which are now enabled by default.

'label' and 'series' requests can be cached now too and this is enabled by default.

All of these are cached to the `results_cache` which is configured in the `query_range` config section.  By default, an in memory cache is used.

#### Write dedupe cache is deprecated

Write dedupe cache is deprecated because it not required by the newer single store indexes ([TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/) and boltdb-shipper).
If you using a [legacy index type](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#index-storage), consider migrating to TSDB (recommended).

#### Embedded cache metric changes

- The following embedded cache metrics are removed. Instead use `loki_cache_fetched_keys`, `loki_cache_hits`, `loki_cache_request_duration_seconds` which instruments requests made to the configured cache (`embeddedcache`, `memcached` or `redis`).
  - `querier_cache_added_total`
  - `querier_cache_gets_total`
  - `querier_cache_misses_total`

- The following embedded cache metrics are renamed:
  - `querier_cache_added_new_total` is renamed to `loki_embeddedcache_added_new_total`
  - `querier_cache_evicted_total` is renamed to `loki_embeddedcache_evicted_total`
  - `querier_cache_entries` is renamed to `loki_embeddedcache_entries`
  - `querier_cache_memory_bytes` is renamed to `loki_embeddedcache_memory_bytes`

- Already deprecated metric `querier_cache_stale_gets_total` is now removed.

#### Metrics namespace

Some Loki metrics started with the prefix `cortex_`. In this release they will be changed so they start with `loki_`. To keep them at `cortex_` change the `metrics_namespace` from the default `loki` to `cortex`. These metrics will be changed:

- `cortex_distributor_ingester_clients`
- `cortex_dns_failures_total`
- `cortex_dns_lookups_total`
- `cortex_dns_provider_results`
- `cortex_frontend_query_range_duration_seconds_bucket`
- `cortex_frontend_query_range_duration_seconds_count`
- `cortex_frontend_query_range_duration_seconds_sum`
- `cortex_ingester_flush_queue_length`
- `cortex_kv_request_duration_seconds_bucket`
- `cortex_kv_request_duration_seconds_count`
- `cortex_kv_request_duration_seconds_sum`
- `cortex_member_consul_heartbeats_total`
- `cortex_prometheus_last_evaluation_samples`
- `cortex_prometheus_notifications_alertmanagers_discovered`
- `cortex_prometheus_notifications_dropped_total`
- `cortex_prometheus_notifications_errors_total`
- `cortex_prometheus_notifications_latency_seconds`
- `cortex_prometheus_notifications_latency_seconds_count`
- `cortex_prometheus_notifications_latency_seconds_sum`
- `cortex_prometheus_notifications_queue_capacity`
- `cortex_prometheus_notifications_queue_length`
- `cortex_prometheus_notifications_sent_total`
- `cortex_prometheus_rule_evaluation_duration_seconds`
- `cortex_prometheus_rule_evaluation_duration_seconds_count`
- `cortex_prometheus_rule_evaluation_duration_seconds_sum`
- `cortex_prometheus_rule_evaluation_failures_total`
- `cortex_prometheus_rule_evaluations_total`
- `cortex_prometheus_rule_group_duration_seconds`
- `cortex_prometheus_rule_group_duration_seconds_count`
- `cortex_prometheus_rule_group_duration_seconds_sum`
- `cortex_prometheus_rule_group_interval_seconds`
- `cortex_prometheus_rule_group_iterations_missed_total`
- `cortex_prometheus_rule_group_iterations_total`
- `cortex_prometheus_rule_group_last_duration_seconds`
- `cortex_prometheus_rule_group_last_evaluation_timestamp_seconds`
- `cortex_prometheus_rule_group_rules`
- `cortex_query_frontend_connected_schedulers`
- `cortex_query_frontend_queries_in_progress`
- `cortex_query_frontend_retries_bucket`
- `cortex_query_frontend_retries_count`
- `cortex_query_frontend_retries_sum`
- `cortex_query_scheduler_connected_frontend_clients`
- `cortex_query_scheduler_connected_querier_clients`
- `cortex_query_scheduler_inflight_requests`
- `cortex_query_scheduler_inflight_requests_count`
- `cortex_query_scheduler_inflight_requests_sum`
- `cortex_query_scheduler_queue_duration_seconds_bucket`
- `cortex_query_scheduler_queue_duration_seconds_count`
- `cortex_query_scheduler_queue_duration_seconds_sum`
- `cortex_query_scheduler_queue_length`
- `cortex_query_scheduler_running`
- `cortex_ring_member_heartbeats_total`
- `cortex_ring_member_tokens_owned`
- `cortex_ring_member_tokens_to_own`
- `cortex_ring_members`
- `cortex_ring_oldest_member_timestamp`
- `cortex_ring_tokens_total`
- `cortex_ruler_client_request_duration_seconds_bucket`
- `cortex_ruler_client_request_duration_seconds_count`
- `cortex_ruler_client_request_duration_seconds_sum`
- `cortex_ruler_clients`
- `cortex_ruler_config_last_reload_successful`
- `cortex_ruler_config_last_reload_successful_seconds`
- `cortex_ruler_config_updates_total`
- `cortex_ruler_managers_total`
- `cortex_ruler_ring_check_errors_total`
- `cortex_ruler_sync_rules_total`

The `metrics_namespace` setting is deprecated already. It will be removed in the next minor release. The default prefix will be `loki` then.

### LogCLI

#### Store for retrieving remote schema

Previously LogCLI used to fetch remote schema from the store configured in `-boltdb.shipper.shared-store` when `-remote-schema` is set to `true`.
A new CLI flag `-schema-store` is introduced as a replacement to configure the store for retrieving remote schema.

## Further reading

To upgrade from one of the versions of Loki 2.x, refer to [Upgrade Loki 2.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-2.x/).

To upgrade from one of the versions of Loki 1.x, refer to [Upgrade Loki 1.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-1.x/).
