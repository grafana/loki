---
title: Filesystem object store
menuTitle: Filesystem object store
description: Describes the features and limitations of using a filesystem object store with Loki.
weight: 300
---
# Filesystem object store

The filesystem object store is the easiest way to get started with Grafana Loki. This topic describes its pros and cons.

The filesystem object store keeps every object, such as chunks, in a directory that you specify. The recommended way to configure this directory is in the `common.storage.filesystem` block:

```yaml
common:
  storage:
    filesystem:
      chunks_directory: /tmp/loki/chunks
      rules_directory: /tmp/loki/rules
```

You can also configure the chunks directory directly under `storage_config`:

```yaml
storage_config:
  filesystem:
    directory: /tmp/loki/
```

Loki creates a folder for every tenant. All the chunks for one tenant are stored in that tenant's folder.

If you run Loki in single-tenant mode, Loki puts all the chunks in a folder named `fake`. This is the synthesized tenant name that Loki uses in single-tenant mode.

See [multi-tenancy](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/) for more information.

{{< admonition type="note" >}}
Loki also supports an alternative, opt-in filesystem client based on `thanos-io/objstore`. Enable it by setting `use_thanos_objstore: true` and configuring `storage_config.object_store.filesystem.dir`. This client will become the default way to configure object store clients in a future release. For configuration examples, see [Thanos storage examples](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/examples/thanos-storage-configs/) and the [storage client migration guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-storage-clients/).
{{< /admonition >}}

## Pros

The filesystem object store is very simple. It requires no additional software to run Loki, and it works with TSDB, which is the recommended index store.

It's great for low volume applications, proof of concepts, and just playing around with Loki.

## Cons

Grafana Labs does not support the filesystem object store for production environments, including for customers who have purchased a support contract.

### Scaling

At some point there is a limit to how many chunks you can store in a single directory. For example, see [issue #1502](https://github.com/grafana/loki/issues/1502), which explains how a Loki user ran into a strange error with about **5.5 million chunk files** in their file store. That issue also describes a workaround for the problem.

Loki writes one chunk per stream, so keeping the number of active streams low reduces the number of chunk files. You can also tune the following ingester settings to reduce how many chunks get flushed, although lower flush rates trade off for higher memory consumption:

- `chunk_target_size` (default 1.5 MB): a target compressed size for each chunk.
- `max_chunk_age` (default 2h): the maximum time a chunk stays in memory before Loki flushes it. Consider increasing this beyond the default.
- `chunk_idle_period` (default 30m): how long an idle chunk stays in memory before Loki flushes it. Consider increasing this to match `max_chunk_age`.

It's still possible to store terabytes of log data with the filesystem store, but keep in mind the limitations on how many files a filesystem can efficiently store in a single directory.

### Durability

The durability of your objects depends entirely on the underlying filesystem. Other object stores, such as S3 and GCS, do a lot of work behind the scenes to offer much higher durability for your data.

### High availability

Running Loki as a cluster is not possible with the filesystem store, unless you share the filesystem in some way, for example over NFS. Using a shared filesystem is likely to give you a poor experience with Loki, just as it does with almost every other application.

### Retention and deletion

When you use the filesystem chunk store, Loki does not delete chunks based on disk usage or free space. Loki only deletes data according to your retention configuration, so you must handle disk-full scenarios yourself, outside of Loki.

Retention-based deletion runs through the compactor. When you enable retention with `compactor.retention_enabled`, you must also set `compactor.delete_request_store`. Set it to `filesystem` to store delete requests in this object store.

For more information, see [Retention](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/) and the [storage retention section](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#retention).
