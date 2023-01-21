---
title: Examples
---
 # Loki Configuration Examples

## almost-zero-dependency.yaml

```yaml
# This is a configuration to deploy Loki depending only on a storage solution 
# for example, an S3-compatible API like MinIO. 
# The ring configuration is based on the gossip memberlist and the index is shipped to storage 
# via Single Store (boltdb-shipper)

auth_enabled: false

server:
  http_listen_port: 3100

distributor:
  ring:
    kvstore:
      store: memberlist

ingester:
  lifecycler:
    ring:
      kvstore:
        store: memberlist
      replication_factor: 1
    final_sleep: 0s
  chunk_idle_period: 5m
  chunk_retain_period: 30s

memberlist:
  abort_if_cluster_join_fails: false

  # Expose this port on all distributor, ingester
  # and querier replicas.
  bind_port: 7946

  # You can use a headless k8s service for all distributor,
  # ingester and querier components.
  join_members:
  - loki-gossip-ring.loki.svc.cluster.local:7946

  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s

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
   active_index_directory: /loki/index
   cache_location: /loki/index_cache
   shared_store: s3

 aws:
   s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
   s3forcepathstyle: true

limits_config:
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h

compactor:
  working_directory: /data/compactor
  shared_store: s3
  compaction_interval: 5m
```


## aws-basic-config-no-creds.yaml

```yaml
# If you don't wish to hard-code S3 credentials you can also configure an EC2
# instance role by changing the `storage_config` section

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
    s3: s3://region/bucket_name
    dynamodb:
      dynamodb_url: dynamodb://region
      
```


## aws-basic-config.yaml

```yaml
# This partial configuration uses S3 for chunk storage and uses DynamoDB for index storage

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


## bos-config.yaml

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


## cassandra-index.yaml

```yaml
# This is a partial config that uses the local filesystem for chunk storage and Cassandra for index storage

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


## complete-local-config.yaml

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
  - from: 2020-05-15
    store: boltdb
    object_store: filesystem
    schema: v11
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
```


## example-schema-config.yaml

```yaml
# Additional example schema configuration for Cassandra

schema_config:
  configs:
    # Starting from 2018-04-15 Loki should store indexes on Cassandra
    # using weekly periodic tables and chunks on filesystem.
    # The index tables will be prefixed with "index_".
  - from: "2018-04-15"
    store: cassandra
    object_store: filesystem
    schema: v11
    index:
        period: 168h
        prefix: index_

  # Starting from 2020-6-15 we moved from filesystem to AWS S3 for storing the chunks.
  - from: "2020-06-15"
    store: cassandra
    object_store: s3
    schema: v11
    index:
        period: 168h
        prefix: index_
        
```


## google-cloud-storage-config.yaml

```yaml
# This partial configuration uses GCS for chunk storage and uses BigTable for index storage

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


## s3-compatible-apis.yaml

```yaml
# S3-compatible APIs such as Ceph Object Storage with an S3-compatible API, can be used.
# If the API supports path-style URLs rather than virtual hosted bucket addressing,
# configure the URL in `storage_config` with the custom endpoint

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


## s3-expanded-config.yaml

```yaml
# S3 configuration supports an expanded configuration. 
# Either an `s3` endpoint URL can be used, or an expanded configuration can be used.

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

