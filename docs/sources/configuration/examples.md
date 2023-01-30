---
title: Examples
description: Loki Configuration Examples
---
 # Examples

## Simple local configuration

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory
  replication_factor: 1
  path_prefix: /tmp/loki

schema_config:
  configs:
  - from: 2020-05-15
    store: boltdb-shipper
    object_store: filesystem
    schema: v11
    index:
      prefix: index_
      period: 24h
```

## Running with s3-compatible API

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: memberlist
  replication_factor: 1
  path_prefix: /loki # Update this accordingly

memberlist:
  # You can use a headless k8s service for all distributor,
  # ingester and querier components.
  join_members:
  - loki-gossip-ring.loki.svc.cluster.local:7946

schema_config:
  configs:
  - from: 2020-05-15
    store: boltdb-shipper
    object_store: s3
    schema: v11
    index:
      prefix: index_
      period: 24h

storage_config:
  boltdb_shipper:
    shared_store: s3
  aws:
    s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
    s3forcepathstyle: true
```

## S3 without credentials

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: aws
    object_store: s3
    schema: v11
    index:
      prefix: loki_
# If you don't wish to hard-code S3 credentials you can also configure an EC2
# instance role by changing the `storage_config` section.
storage_config:
  aws:
    s3: s3://region/bucket_name
    dynamodb:
      dynamodb_url: dynamodb://region
```


## Storing chunks on S3 and indexes on DynamoDB

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: aws
    object_store: s3
    schema: v11
    index:
      prefix: loki_
storage_config:
  aws:
    s3: s3://access_key:secret_access_key@region/bucket_name
    dynamodb:
      dynamodb_url: dynamodb://access_key:secret_access_key@region
```

## Storing data on BOS (Baidu Object Storage)

```yaml
schema_config:
  configs:
    - from: 2020-05-15
      store: boltdb-shipper
      object_store: bos
      schema: v11
      index:
        prefix: index_
        period: 24h

storage_config:
  boltdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
    shared_store: bos

  bos:
    bucket_name: bucket_name_1
    endpoint: bj.bcebos.com
    access_key_id: access_key_id
    secret_access_key: secret_access_key

compactor:
  working_directory: /tmp/loki/compactor
  shared_store: bos
```

## Storing indexes on Cassandra

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: cassandra
    object_store: filesystem
    schema: v11
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

## Migrating from backend stores

```yaml
schema_config:
  configs:
    # Starting from 2018-04-15 Loki should store indexes on Cassandra and chunks on filesystem.
    # The index tables will be prefixed with "index_".
  - from: "2018-04-15"
    store: cassandra
    object_store: filesystem
    schema: v11
    index:
        period: 168h
        prefix: index_

  # Starting from 2020-6-15 chunks are stored on AWS S3 instead of filesystem.
  - from: "2020-06-15"
    store: cassandra
    object_store: s3
    schema: v11
    index:
        period: 168h
        prefix: index_
```

## GoogleCloudStorage usage

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: bigtable
    object_store: gcs
    schema: v11
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

## S3-compatible API usage (ex: Ceph Storage)

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: aws
    object_store: s3
    schema: v11
    index:
      prefix: loki_
storage_config:
  aws:
    # If the API supports path-style URLs rather than virtual hosted bucket addressing,
    # configure the URL with the custom endpoint.
    s3: s3://access_key:secret_access_key@region/bucket_name
    dynamodb:
      dynamodb_url: dynamodb://access_key:secret_access_key@region
      
```

## S3 configuration expanded

```yaml
schema_config:
  configs:
  - from: 2020-05-15
    store: aws
    object_store: s3
    schema: v11
    index:
      prefix: loki_
storage_config:
  aws:
    bucketnames: bucket_name1, bucket_name2
    endpoint: s3.endpoint.com
    region: s3_region
    access_key_id: s3_access_key_id
    secret_access_key: s3_secret_access_key
    insecure: false
    sse_encryption: false
    http_config:
      idle_conn_timeout: 90s
      response_header_timeout: 0s
      insecure_skip_verify: false
    s3forcepathstyle: true
```
