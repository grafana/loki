# Loki Configuration Examples

1. [Complete Local Config](#complete-local-config)
2. [Google Cloud Storage](#google-cloud-storage)
3. [Cassandra Index](#cassandra-index)
4. [AWS](#aws)

## Complete Local config

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

ingester:
  lifecycler:
    address: 127.0.0.1
    ring:
      kvstore:
        store: inmemory
      replication_factor: 1
    final_sleep: 0s
  chunk_idle_period: 5m
  chunk_retain_period: 30s

schema_config:
  configs:
  - from: 2018-04-15
    store: boltdb
    object_store: filesystem
    schema: v9
    index:
      prefix: index_
      period: 168h

storage_config:
  boltdb:
    directory: /tmp/loki/index

  filesystem:
    directory: /tmp/loki/chunks

limits_config:
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h

chunk_store_config:
  max_look_back_period: 0

table_manager:
  chunk_tables_provisioning:
    inactive_read_throughput: 0
    inactive_write_throughput: 0
    provisioned_read_throughput: 0
    provisioned_write_throughput: 0
  index_tables_provisioning:
    inactive_read_throughput: 0
    inactive_write_throughput: 0
    provisioned_read_throughput: 0
    provisioned_write_throughput: 0
  retention_deletes_enabled: false
  retention_period: 0
```

## Google Cloud Storage

This is partial config that uses GCS and Bigtable for the chunk and index
stores, respectively.

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: bigtable
    object_store: gcs
    schema: v9
    index:
      prefix: loki_index_
      period: 168h

storage_config:
  bigtable:
    instance: BIGTABLE_INSTANCE
    project: BIGTABLE_PROJECT
  gcs:
    bucket_name: GCS_BUCKET_NAME
```

## Cassandra Index

This is a partial config that uses the local filesystem for chunk storage and
Cassandra for the index storage:

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: cassandra
    object_store: filesystem
    schema: v9
    index:
      prefix: cassandra_table
      period: 168h

storage_config:
  cassandra:
    username: cassandra
    password: cassandra
    addresses: 127.0.0.1
    auth: true
    keyspace: lokiindex

  filesystem:
    directory: /tmp/loki/chunks
```

## AWS

This is a partial config that uses S3 for chunk storage and DynamoDB for the
index storage:

```yaml
schema_config:
  configs:
    - from: 2018-04-15
      store: dynamo
      object_store: s3
      schema: v9
      index:
        prefix: dynamodb_table_name
        period: 0
storage_config:
  aws:
    s3: s3://access_key:secret_access_key@region/bucket_name
    dynamodbconfig:
      dynamodb: dynamodb://access_key:secret_access_key@region
```

If you don't wish to hard-code S3 credentials, you can also configure an EC2
instance role by changing the `storage_config` section:

```yaml
storage_config:
  aws:
    s3: s3://region/bucket_name
    dynamodbconfig:
      dynamodb: dynamodb://region
```

### S3-compatible APIs

S3-compatible APIs (e.g., Ceph Object Storage with an S3-compatible API) can be
used. If the API supports path-style URL rather than virtual hosted bucket
addressing, configure the URL in `storage_config` with the custom endpoint:

```yaml
storage_config:
  aws:
    s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
    s3forcepathstyle: true
```
