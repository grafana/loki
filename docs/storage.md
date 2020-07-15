# Storage

Loki uses a two pronged strategy regarding storage, which is responsible for both it's limitations and it's advantages. The main idea is that logs are large and traditional indexing strategies are prohibitively expensive and complex to run at scale. This often brings along ancillary procedure costs in the form of schema design, index management/rotation, backup/restore protocols, etc. Instead, Loki stores all the its log content unindexed in object storage. It then uses the Prometheus label paradigm along with a small but specialized index store to allow lookup, matching, and filtering based on the these labels. When a set of unique key/value label pairs are combined with their logs, we call this a _log stream_, which is generally analagous to a log file on disk. It may have labels like `{app="api", env="production", filename="/var/logs/app.log"}`, which together uniqely identify it. The object storage is responsible for storing the compressed logs cheaply while the index takes care of storing these labels in a way that enables fast, effective querying.

* [Chunk Clients](#Implementations---Chunks)
  * [Cassandra](#Cassandra)
  * [GCS](#GCS)
  * [File System](#File-System)
  * [S3](#S3)
  * [Notable Mentions](#Notable-Mentions)
* [Index Clients](#Implementations---Index)
  * [Cassandra](#Cassandra)
  * [BigTable](#BigTable)
  * [DynamoDB](#DynamoDB)
  * [BoltDB](#BoltDB)
* [Period Configs](#Period-Configs)
* [Table Manger](#Table-Manager)
* [Upgrading Schemas](#Upgrading-Schemas)
* [Retention](#Retention)
* [Examples](Examples)
  * [Proof of concept/local development (boltdb+filesystem)](Proof-of-concept/local-development-(boltdb+filesystem))
  * [GCP deployment (GCS+BigTable)](GCP-deployment-(GCS+BigTable))
  * [AWS deployment (S3+DynamoDB)](AWS-deployment-(S3+DynamoDB))
  * [On prem deployment (Cassandra+Cassandra)](On-prem-deployment-(Cassandra+Cassandra))
  * [On prem deployment (Cassandra+MinIO)](On-prem-deployment-(Cassandra+MinIO))
  * []()
  * []()

## Implementations - Chunks

### Cassandra

Cassandra is a popular database and one of Loki's possible chunk stores. 

### GCS

GCS is a hosted object store offered by Google. It is a good candidate for a managed object store, especially when you're already running on GCP, and is production safe.

### File System

The file system is the simplest backend for chunks, although it's also susceptible to data loss as it's unreplicated. This is common for single binary deployments though, as well as for those trying out loki or doing local development on the project.

### S3

S3 is AWS's hosted object store. It is a good candidate for a managed object store, especially when you're already running on AWS, and is production safe.

#### Rate Limiting


### Notable Mentions

You may use any subsitutable services, such as those that implement the S3 API like [MinIO](https://min.io/).

## Implementations - Index

### Cassandra

Cassandra can also be utilized for the index store and asides from the experimental `boltdb-shipper`, it's the only non-cloud offering that can be used for the index that's horizontally scalable and has configurable replication. It's a good candidate when you already run Cassandra, are running on-prem, or do not wish to use a managed cloud offering.

### BigTable

Bigtable is a cloud database offered by Google. It is a good candidate for a managed index store if you're already using it (due to it's heavy fixed costs) or wish to run in GCP.

### DynamoDB

DynamoDB is a cloud database offered by AWS. It is a good candidate for a managed index store, especially if you're already running in AWS.

### BoltDB

BoltDB is an embedded database on disk. It is not replicated and thus cannot be used for high availability or clustered Loki deployments, but is commonly paired with a `filesystem` chunk store for proof of concept deployments, trying out Loki, and development. There is also an experimental mode, the [boltdb-shipper](./operations/storage/boltdb-shipper.md), which aims to support clustered deployments using `boltdb` as an index.

## Period Configs

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

## Table Manager

## Upgrading Schemas

## Retention

table manager rotates index and the fs object store, but otherwise object store retention must be configured at the object store layer.

## Examples

### Proof of concept/local development (boltdb+filesystem)

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
    bucketnames: <bucket>
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
