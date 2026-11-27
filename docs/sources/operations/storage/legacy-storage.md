---
title: Legacy storage
menuTitle:
description: Describes deprecated legacy storage options for Loki that are superseded by single store.
weight: 1000
---

# Legacy storage

{{< admonition type="warning" >}}
The concepts described on this page are considered legacy and pre-date the single store storage introduced in Loki 2.0.
Support for these storage options is deprecated and will be removed in Loki 4.0. Do not use them for new
installations. This page is meant for informational purposes only, to help you if you are upgrading an older
installation to a single store.

For current storage guidance, refer to:

- [Storage](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/)
- [Storage schema](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/schema/)
- [Single Store BoltDB (boltdb-shipper)](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/boltdb-shipper/)
- [Table manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/)
{{< /admonition >}}

The **chunk store** is the Loki long-term data store, designed to support
interactive querying and sustained writing without the need for background
maintenance tasks. It consists of:

- An index for the chunks. This index can be backed by:
  - [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
  - [Google Bigtable](https://cloud.google.com/bigtable)
  - [Apache Cassandra](https://cassandra.apache.org)
  - Local [BoltDB](https://github.com/boltdb/bolt) files. This option does not work when running Loki as a cluster, because the index is only stored on local disk.
- A key-value (KV) store for the chunk data itself, which can be:
  - [Amazon DynamoDB](https://aws.amazon.com/dynamodb)
  - [Google Bigtable](https://cloud.google.com/bigtable)
  - [Apache Cassandra](https://cassandra.apache.org)
  - [Amazon S3](https://aws.amazon.com/s3)
  - [Google Cloud Storage](https://cloud.google.com/storage/)

A `grpc-store` backend also exists for both the index and the chunk data. It lets a separate, custom service
implement the storage over gRPC. It is deprecated in the same way as the backends listed above.

In the [`schema_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#schema_config) block, the index
backend is set with the `store` field and the chunk data backend is set with the `object_store` field. In the following example, the first period uses DynamoDB for the index and S3 for chunks. The second period migrates to the recommended `tsdb` index and schema `v13`:

```yaml
schema_config:
  configs:
    - from: 2020-01-01
      store: aws-dynamo
      object_store: s3
      schema: v11
      index:
        prefix: loki_index_
        period: 168h
    - from: 2024-04-01
      store: tsdb
      object_store: s3
      schema: v13
      index:
        prefix: index_
        period: 24h
```

Loki keeps reading the older data using the legacy settings, and writes all new data using the current period. Refer to [Storage schema](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/schema/) for the full procedure, including how to choose the `from` date.

{{< admonition type="note" >}}
Unlike the other core components of Loki, the chunk store is not a separate service, job, or process. It is a library embedded into any Loki component that needs to read or write Loki data, such as the
[ingester](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#ingester), the
[querier](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#querier), the ruler, and the index gateway.
{{< /admonition >}}

{{< admonition type="note" >}}
Loki 3.x can still read data from these legacy backends. For each period that uses one of them, Loki logs a deprecation warning at startup, but it starts normally.

Two limits apply if a legacy backend is used by the **active** period, which is the most recent period in `schema_config`:

- Structured metadata and native OpenTelemetry Protocol (OTLP) ingestion require the `tsdb` index type and schema `v13` or newer. Because `allow_structured_metadata` defaults to `true`, Loki reports a configuration error and does not start. To keep a legacy backend as the active period, you must set `allow_structured_metadata: false` in the `limits_config` block.
- The legacy chunk backends are not supported by the Thanos object storage client. If you set `use_thanos_objstore: true` in `storage_config`, Loki rejects them in `object_store` as a configuration error. Before you [migrate to the Thanos object storage client](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-storage-clients/), migrate away from these backends.

The recommended path is to add a new period that uses `tsdb` and schema `v13`, as shown in the previous example.
{{< /admonition >}}

The chunk store relies on a unified interface to the
"[NoSQL](https://en.wikipedia.org/wiki/NoSQL)" stores (DynamoDB, Bigtable, and
Cassandra) that can be used to back the chunk store index. This interface
assumes that the index is a collection of entries keyed by:

- A **hash key**. This is required for *all* reads and writes.
- A **range key**. This is required for writes and can be omitted for reads,
which can be queried by prefix or range.

The interface works somewhat differently across the supported databases:

- DynamoDB supports range and hash keys natively. Index entries are thus
  modelled directly as DynamoDB entries, with the hash key as the distribution
  key and the range as the DynamoDB range key.
- For Bigtable and Cassandra, index entries are modelled as individual column
  values. The hash key becomes the row key and the range key becomes the column
  key.

A set of schemas are used to map the matchers and label sets used on reads and
writes to the chunk store into appropriate operations on the index. Schemas have
been added as Loki has evolved, mainly in an attempt to better load balance
writes and improve query performance.
