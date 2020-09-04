---
title: Storage
weight: 1010
---
# Storage

Loki uses a two pronged strategy regarding storage, which is responsible for both it's limitations and it's advantages. The main idea is that logs are large and traditional indexing strategies are prohibitively expensive and complex to run at scale. This often brings along ancillary procedure costs in the form of schema design, index management/rotation, backup/restore protocols, etc. Instead, Loki stores all the its log content unindexed in object storage. It then uses the Prometheus label paradigm along with a small but specialized index store to allow lookup, matching, and filtering based on the these labels. When a set of unique key/value label pairs are combined with their logs, we call this a _log stream_, which is generally analagous to a log file on disk. It may have labels like `{app="api", env="production", filename="/var/logs/app.log"}`, which together uniqely identify it. The object storage is responsible for storing the compressed logs cheaply while the index takes care of storing these labels in a way that enables fast, effective querying.

- [Storage](#storage)
  - [Implementations - Chunks](#implementations---chunks)
    - [Cassandra](#cassandra)
    - [GCS](#gcs)
    - [File System](#file-system)
    - [S3](#s3)
    - [Notable Mentions](#notable-mentions)
  - [Implementations - Index](#implementations---index)
    - [Cassandra](#cassandra-1)
    - [BigTable](#bigtable)
    - [DynamoDB](#dynamodb)
      - [Rate Limiting](#rate-limiting)
    - [BoltDB](#boltdb)
  - [Period Configs](#period-configs)
  - [Table Manager](#table-manager)
    - [Provisioning](#provisioning)
  - [Upgrading Schemas](#upgrading-schemas)
  - [Retention](#retention)
  - [Examples](#examples)
    - [Single machine/local development (boltdb+filesystem)](#single-machinelocal-development-boltdbfilesystem)
    - [GCP deployment (GCS+BigTable)](#gcp-deployment-gcsbigtable)
    - [AWS deployment (S3+DynamoDB)](#aws-deployment-s3dynamodb)
    - [On prem deployment (Cassandra+Cassandra)](#on-prem-deployment-cassandracassandra)
    - [On prem deployment (Cassandra+MinIO)](#on-prem-deployment-cassandraminio)

## Implementations - Chunks

### Cassandra

Cassandra is a popular database and one of Loki's possible chunk stores and is production safe.

### GCS

GCS is a hosted object store offered by Google. It is a good candidate for a managed object store, especially when you're already running on GCP, and is production safe.

### File System

The file system is the simplest backend for chunks, although it's also susceptible to data loss as it's unreplicated. This is common for single binary deployments though, as well as for those trying out loki or doing local development on the project. It is similar in concept to many Prometheus deployments where a single Prometheus is responsible for monitoring a fleet.

### S3

S3 is AWS's hosted object store. It is a good candidate for a managed object store, especially when you're already running on AWS, and is production safe.

### Notable Mentions

You may use any subsitutable services, such as those that implement the S3 API like [MinIO](https://min.io/).

## Implementations - Index

### Cassandra

Cassandra can also be utilized for the index store and aside from the experimental [boltdb-shipper](../operations/storage/boltdb-shipper/), it's the only non-cloud offering that can be used for the index that's horizontally scalable and has configurable replication. It's a good candidate when you already run Cassandra, are running on-prem, or do not wish to use a managed cloud offering.

### BigTable

Bigtable is a cloud database offered by Google. It is a good candidate for a managed index store if you're already using it (due to it's heavy fixed costs) or wish to run in GCP.

### DynamoDB

DynamoDB is a cloud database offered by AWS. It is a good candidate for a managed index store, especially if you're already running in AWS.

#### Rate Limiting

DynamoDB is susceptible to rate limiting, particularly due to overconsuming what is called [provisioned capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadWriteCapacityMode.html). This can be controlled via the [provisioning](#provisioning) configs in the table manager.

### BoltDB

BoltDB is an embedded database on disk. It is not replicated and thus cannot be used for high availability or clustered Loki deployments, but is commonly paired with a `filesystem` chunk store for proof of concept deployments, trying out Loki, and development. There is also an experimental mode, the [boltdb-shipper](../operations/storage/boltdb-shipper/), which aims to support clustered deployments using `boltdb` as an index.

## Period Configs

Loki aims to be backwards compatible and over the course of it's development has had many internal changes that facilitate better and more efficient storage/querying. Loki allows incrementally upgrading to these new storage _schemas_ and can query across them transparently. This makes upgrading a breeze. For instance, this is what it looks like when migrating from the v10 -> v11 schemas starting 2020-07-01:

```yaml
schema_config:
  configs:
    - from: 2019-07-01
      store: boltdb
      object_store: filesystem
      schema: v10
      index:
        prefix: index_
        period: 168h
    - from: 2020-07-01
      store: boltdb
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 168h
```

For all data ingested before 2020-07-01, Loki used the v10 schema and then switched after that point to the more effective v11. This dramatically simplifies upgrading, ensuring it's simple to take advantages of new storage optimizations. These configs should be immutable for as long as you care about retention.

## Table Manager

One of the subcomponents in Loki is the `table-manager`. It is responsible for pre-creating and expiring index tables. This helps partition the writes and reads in loki across a set of distinct indices in order to prevent unbounded growth.

```yaml
table_manager:
  # The retention period must be a multiple of the index / chunks
  # table "period" (see period_config).
  retention_deletes_enabled: true
  # This is 15 weeks retention, based on the 168h (1week) period durations used in the rest of the examples.
  retention_period: 2520h
```

For more information, see the table manager [doc](../operations/storage/table-manager/).

### Provisioning

In the case of AWS DynamoDB, you'll likely want to tune the provisioned throughput for your tables as well. This is to prevent your tables being rate limited on one hand and assuming unnecessary cost on the other. By default Loki uses a [provisioned capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadWriteCapacityMode.html) strategy for DynamoDB tables like so:

```
table_manager:
  index_tables_provisioning:
    # Read/write throughput requirements for the current table
    # (the table which would handle writes/reads for data timestamped at the current time)
    provisioned_write_throughput: <int> | default = 3000
    provisioned_read_throughput: <int> | default = 300

    # Read/write throughput requirements for non-current tables
    inactive_write_throughput: <int> | default = 1
    inactive_read_throughput: <int> | Default = 300
```

Note, there are a few other DynamoDB provisioning options including DynamoDB autoscaling and on-demand capacity. See the [docs](../configuration/#provision_config) for more information.

## Upgrading Schemas

When a new schema is released and you want to gain the advantages it provides, you can! Loki can transparently query & merge data from across schema boundaries so there is no disruption of service and upgrading is easy.

First, you'll want to create a new [period_config](../configuration#period_config) entry in your [schema_config](../configuration#schema_config). The important thing to remember here is to set this at some point in the _future_ and then roll out the config file changes to Loki. This allows the table manager to create the required table in advance of writes and ensures that existing data isn't queried as if it adheres to the new schema.

As an example, let's say it's 2020-07-14 and we want to start using the `v11` schema on the 20th:
```yaml
schema_config:
  configs:
    - from: 2019-07-14
      store: boltdb
      object_store: filesystem
      schema: v10
      index:
        prefix: index_
        period: 168h
    - from: 2020-07-20
      store: boltdb
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 168h
```

It's that easy; we just created a new entry starting on the 20th.

## Retention

With the exception of the `filesystem` chunk store, Loki will not delete old chunk stores. This is generally handled instead by configuring TTLs (time to live) in the chunk store of your choice (bucket lifecycles in S3/GCS, and TTLs in Cassandra). Neither will Loki currently delete old data when your local disk fills when using the `filesystem` chunk store -- deletion is only determined by retention duration.

We're interested in adding targeted deletion in future Loki releases (think tenant or stream level granularity) and may include other strategies as well.

For more information, see the configuration [docs](../operations/storage/retention/).


## Examples

### Single machine/local development (boltdb+filesystem)

```yaml
storage_config:
  boltdb:
    directory: /tmp/loki/index
  filesystem:
    directory: /tmp/loki/chunks

schema_config:
  configs:
    - from: 2020-07-01
      store: boltdb
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 168h
```

### GCP deployment (GCS+BigTable)

```yaml
storage_config:
  bigtable:
      instance: <instance>
      project: <project>
  gcs:
      bucket_name: <bucket>

schema_config:
  configs:
    - from: 2020-07-01
      store: bigtable
      object_store: gcs
      schema: v11
      index:
        prefix: index_
        period: 168h
```

### AWS deployment (S3+DynamoDB)

```yaml
storage_config:
  aws:
    s3: s3://<access_key>:<uri-encoded-secret-access-key>@<region>
    bucketnames: <bucket1,bucket2>
    dynamodb:
      dynamodb_url: dynamodb://<access_key>:<uri-encoded-secret-access-key>@<region>

schema_config:
  configs:
    - from: 2020-07-01
      store: aws
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 168h
```

If you don't wish to hard-code S3 credentials, you can also configure an EC2
instance role by changing the `storage_config` section:

```yaml
storage_config:
  aws:
    s3: s3://region
    bucketnames: <bucket1,bucket2>
    dynamodb:
      dynamodb_url: dynamodb://region
```

### On prem deployment (Cassandra+Cassandra)

```yaml
storage_config:
  cassandra:
    addresses: <comma-separated-IPs-or-hostnames>
    keyspace: <keyspace>
    auth: <true|false>
    username: <username> # only applicable when auth=true
    password: <password> # only applicable when auth=true

schema_config:
  configs:
    - from: 2020-07-01
      store: cassandra
      object_store: cassandra
      schema: v11
      index:
        prefix: index_
        period: 168h
      chunks:
        prefix: chunk_
        period: 168h

```

### On prem deployment (Cassandra+MinIO)

We configure MinIO by using the AWS config because MinIO implements the S3 API:

```yaml
storage_config:
  aws:
    # Note: use a fully qualified domain name, like localhost.
    # full example: http://loki:supersecret@localhost.:9000
    s3: http<s>://<username>:<secret>@<fqdn>:<port>
    s3forcepathstyle: true
  cassandra:
    addresses: <comma-separated-IPs-or-hostnames>
    keyspace: <keyspace>
    auth: <true|false>
    username: <username> # only applicable when auth=true
    password: <password> # only applicable when auth=true

schema_config:
  configs:
    - from: 2020-07-01
      store: cassandra
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 168h
```
