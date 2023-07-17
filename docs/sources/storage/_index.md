---
title: Storage
description: Storage
weight: 1010
---
# Storage

Unlike other logging systems, Grafana Loki is built around the idea of only indexing
metadata about your logs: labels (just like Prometheus labels). Log data itself
is then compressed and stored in chunks in object stores such as S3 or GCS, or
even locally on the filesystem. A small index and highly compressed chunks
simplifies the operation and significantly lowers the cost of Loki.

Until Loki 2.0, index data was stored in a separate index.

Loki 2.0 brings an index mechanism named 'boltdb-shipper' and is what we now call Single Store Loki.
This index type only requires one store, the object store, for both the index and chunks.
More detailed information can be found on the [operations page]({{< relref "../operations/storage/boltdb-shipper.md" >}}).

Some more storage details can also be found in the [operations section]({{< relref "../operations/storage/_index.md" >}}).

## Implementations - Chunks

### Cassandra

Cassandra is a popular database and one of Loki's possible chunk stores and is production safe.

### GCS

GCS is a hosted object store offered by Google. It is a good candidate for a managed object store, especially when you're already running on GCP, and is production safe.

### File System

The file system is the simplest backend for chunks, although it's also susceptible to data loss as it's unreplicated. This is common for single binary deployments though, as well as for those trying out loki or doing local development on the project. It is similar in concept to many Prometheus deployments where a single Prometheus is responsible for monitoring a fleet.

### S3

S3 is AWS's hosted object store. It is a good candidate for a managed object store, especially when you're already running on AWS, and is production safe.

### Azure Blob Storage

Blob Storage is Microsoft Azure's hosted object store. It is a good candidate for a managed object store, especially when you're already running on Azure, and is production safe.
You can authenticate Blob Storage access by using a storage account name and key or by using a Service Principal.

### IBM Cloud Object Storage (COS)
[COS](https://www.ibm.com/cloud/object-storage) is IBM Cloud hosted object store. It is a good candidate for a managed object store, especially when you're already running on IBM Cloud, and is production safe.

### Notable Mentions

You may use any substitutable services, such as those that implement the S3 API like [MinIO](https://min.io/).

## Implementations - Index

### Single-Store

Single-Store refers to the using object storage as the storage medium for both Loki's index as well as its data ("chunks"). There are two supported modes:

#### tsdb (recommended)

Starting in Loki v2.8, the [TSDB index store]({{< relref "../operations/storage/tsdb" >}}) improves query performance, reduces TCO and has the same feature parity as "boltdb-shipper".

#### BoltDB (deprecated)

Also known as "boltdb-shipper" during development (and is still the schema `store` name). The single store configurations for Loki utilize the chunk store for both chunks and the index, requiring just one store to run Loki.

Performance is comparable to a dedicated index type while providing a much less expensive and less complicated deployment.

### Cassandra

Cassandra can also be utilized for the index store and aside from the [boltdb-shipper]({{< relref "../operations/storage/boltdb-shipper" >}}), it's the only non-cloud offering that can be used for the index that's horizontally scalable and has configurable replication. It's a good candidate when you already run Cassandra, are running on-prem, or do not wish to use a managed cloud offering.

### BigTable

Bigtable is a cloud database offered by Google. It is a good candidate for a managed index store if you're already using it (due to its heavy fixed costs) or wish to run in GCP.

### DynamoDB

DynamoDB is a cloud database offered by AWS. It is a good candidate for a managed index store, especially if you're already running in AWS.

#### Rate Limiting

DynamoDB is susceptible to rate limiting, particularly due to overconsuming what is called [provisioned capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadWriteCapacityMode.html). This can be controlled via the [provisioning](#provisioning) configs in the table manager.

### BoltDB

BoltDB is an embedded database on disk. It is not replicated and thus cannot be used for high availability or clustered Loki deployments, but is commonly paired with a `filesystem` chunk store for proof of concept deployments, trying out Loki, and development. The [boltdb-shipper]({{< relref "../operations/storage/boltdb-shipper" >}}) aims to support clustered deployments using `boltdb` as an index.

### Azure Storage Account

An Azure storage account contains all of your Azure Storage data objects: blobs, file shares, queues, tables, and disks.

## Schema Configs

Loki aims to be backwards compatible and over the course of its development has had many internal changes that facilitate better and more efficient storage/querying. Loki allows incrementally upgrading to these new storage _schemas_ and can query across them transparently. This makes upgrading a breeze. For instance, this is what it looks like when migrating from the v10 -> v11 schemas starting 2020-07-01:

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

For all data ingested before 2020-07-01, Loki used the v10 schema and then switched after that point to the more effective v11. This dramatically simplifies upgrading, ensuring it's simple to take advantage of new storage optimizations. These configs should be immutable for as long as you care about retention.

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

For more information, see the [table manager]({{< relref "../operations/storage/table-manager" >}}) documentation.

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

Note, there are a few other DynamoDB provisioning options including DynamoDB autoscaling and on-demand capacity. See the [provisioning configuration]({{< relref "../configure#table_manager" >}}) in the `table_manager` block documentation for more information.

## Upgrading Schemas

When a new schema is released and you want to gain the advantages it provides, you can! Loki can transparently query & merge data from across schema boundaries so there is no disruption of service and upgrading is easy.

First, you'll want to create a new [period_config]({{< relref "../configure#period_config" >}}) entry in your [schema_config]({{< relref "../configure#schema_config" >}}). The important thing to remember here is to set this at some point in the _future_ and then roll out the config file changes to Loki. This allows the table manager to create the required table in advance of writes and ensures that existing data isn't queried as if it adheres to the new schema.

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

For more information, see the [retention configuration]({{< relref "../operations/storage/retention" >}}) documentation.


## Examples

### Single machine/local development (boltdb+filesystem)

[The repo contains a working example](https://github.com/grafana/loki/blob/main/cmd/loki/loki-local-config.yaml), you may want to checkout a tag of the repo to make sure you get a compatible example.

### GCP deployment (GCS Single Store)

```yaml
storage_config:
  boltdb_shipper:
    active_index_directory: /loki/boltdb-shipper-active
    cache_location: /loki/boltdb-shipper-cache
    cache_ttl: 24h         # Can be increased for faster performance over longer query periods, uses more disk space
    shared_store: gcs
  gcs:
      bucket_name: <bucket>

schema_config:
  configs:
    - from: 2020-07-01
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: index_
        period: 24h
```

### AWS deployment (S3 Single Store)

```yaml
storage_config:
  boltdb_shipper:
    active_index_directory: /loki/boltdb-shipper-active
    cache_location: /loki/boltdb-shipper-cache
    cache_ttl: 24h         # Can be increased for faster performance over longer query periods, uses more disk space
    shared_store: s3
  aws:
    s3: s3://<access_key>:<uri-encoded-secret-access-key>@<region>
    bucketnames: <bucket1,bucket2>

schema_config:
  configs:
    - from: 2020-07-01
      store: boltdb-shipper
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 24h
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

The role should have a policy with the following permissions attached.

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "LokiStorage",
            "Effect": "Allow",
            "Principal": {
                "AWS": [
                    "arn:aws:iam::<account_ID>"
                ]
            },
            "Action": [
                "s3:ListBucket",
                "s3:PutObject",
                "s3:GetObject",
                "s3:DeleteObject"
            ],
            "Resource": [
                "arn:aws:s3:::<bucket_name>",
                "arn:aws:s3:::<bucket_name>/*"
            ]
        }
    ]
}
```

**To setup an S3 bucket and an IAM role and policy:** 

This guide assumes a provisioned EKS cluster.

1. Checkout the Loki repository and navigate to [production/terraform/modules/s3](https://github.com/grafana/loki/tree/main/production/terraform/modules/s3).

2. Initialize Terraform `terraform init`.

3. Export the AWS profile and region if not done so:

   ```
   export AWS_PROFILE=<profile in ~/.aws/config>
   export AWS_REGION=<region of EKS cluster>
   ```

4. Save the OIDC provider in an enviroment variable:

   ```
   oidc_provider=$(aws eks describe-cluster --name <EKS cluster> --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
   ```

   See the [IAM OIDC provider guide](https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html) for a guide for creating a provider.

5. Apply the Terraform module `terraform -var region="$AWS_REGION" -var cluster_name=<EKS cluster> -var oidc_id="$oidc_provider"`

   Note, the bucket name defaults to `loki-data` but can be changed via the
   `bucket_name` variable.


### IBM Cloud Object Storage

```yaml
schema_config:
  configs:
    - from: 2020-10-01
      index:
        period: 24h
        prefix: loki_index_
      object_store: "cos"
      schema: v11
      store: "boltdb-shipper"

storage_config:
  cos:
    bucketnames: <bucket1, bucket2>
    endpoint: <endpoint>
    api_key: <api_key_to_authenticate_with_cos>
    region: <region>
    service_instance_id: <cos_service_instance_id>
    auth_endpoint: <iam_endpoint_for_authentication>
```

### On prem deployment (Cassandra+Cassandra)

**Keeping this for posterity, but this is likely not a common config. Cassandra should work and could be faster in some situations but is likely much more expensive.**

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

### On prem deployment (MinIO Single Store)

We configure MinIO by using the AWS config because MinIO implements the S3 API:

```yaml
storage_config:
  aws:
    # Note: use a fully qualified domain name, like localhost.
    # full example: http://loki:supersecret@localhost.:9000
    s3: http<s>://<username>:<secret>@<fqdn>:<port>
    s3forcepathstyle: true
  boltdb_shipper:
    active_index_directory: /loki/boltdb-shipper-active
    cache_location: /loki/boltdb-shipper-cache
    cache_ttl: 24h         # Can be increased for faster performance over longer query periods, uses more disk space
    shared_store: s3

schema_config:
  configs:
    - from: 2020-07-01
      store: boltdb-shipper
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 24h
```

### Azure Storage Account

#### Using account name and key

```yaml
schema_config:
  configs:
  - from: "2020-12-11"
    index:
      period: 24h
      prefix: index_
    object_store: azure
    schema: v11
    store: boltdb-shipper
storage_config:
  azure:
    # Your Azure storage account name
    account_name: <account-name>
    # For the account-key, see docs: https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage?tabs=azure-portal
    account_key: <account-key>
    # See https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction#containers
    container_name: <container-name>
    use_managed_identity: <true|false>
    # Providing a user assigned ID will override use_managed_identity
    user_assigned_id: <user-assigned-identity-id>
    request_timeout: 0
    # Configure this if you are using private azure cloud like azure stack hub and will use this endpoint suffix to compose container & blob storage URL. Ex: https://account_name.endpoint_suffix/container_name/blob_name
    endpoint_suffix: <endpoint-suffix>
  boltdb_shipper:
    active_index_directory: /data/loki/boltdb-shipper-active
    cache_location: /data/loki/boltdb-shipper-cache
    cache_ttl: 24h
    shared_store: azure
  filesystem:
    directory: /data/loki/chunks
```

#### Using a service principal

```yaml
schema_config:
  configs:
  - from: "2020-12-11"
    index:
      period: 24h
      prefix: index_
    object_store: azure
    schema: v11
    store: boltdb-shipper
storage_config:
  azure:
    use_service_principal: true
    # Azure tenant ID used to authenticate through Azure OAuth
    tenant_id : <tenant-id>
    # Azure Service Principal ID
    client_id: <client-id>
    # Azure Service Principal secret key
    client_secret: <client-secret>
    # See https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction#containers
    container_name: <container-name>
    request_timeout: 0
  boltdb_shipper:
    active_index_directory: /data/loki/boltdb-shipper-active
    cache_location: /data/loki/boltdb-shipper-cache
    cache_ttl: 24h
    shared_store: azure
  filesystem:
    directory: /data/loki/chunks
```
