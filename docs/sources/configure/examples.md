---

title: Examples
menuTitle:  
description: Loki Configuration Examples
aliases: 
  - ../configuration/examples
  - ../configure/examples
weight:  100
---
 # Examples

## alibaba-cloud-storage-config.yaml

```yaml

# This partial configuration uses Alibaba for chunk storage

schema_config:
  configs:
  - from: 2020-05-15
    object_store: alibabacloud
    schema: v11
    index:
      prefix: loki_index_
      period: 168h

storage_config:
  alibabacloud:
    bucket: <bucket>
    endpoint: <endpoint>
    access_key_id: <access_key_id>
    secret_access_key: <secret_access_key>

```


## 1-Local-Configuration-Example.yaml

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


## 2-S3-Cluster-Example.yaml

```yaml

# This is a complete configuration to deploy Loki backed by a s3-Comaptible API
# like MinIO for storage. Loki components will use memberlist ring to shard and
# the index will be shipped to storage via boltdb-shipper.

auth_enabled: false

server:
  http_listen_port: 3100

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: memberlist
  replication_factor: 1
  path_prefix: /loki # Update this accordingly, data will be stored here.

memberlist:
  join_members:
  # You can use a headless k8s service for all distributor, ingester and querier components.
  - loki-gossip-ring.loki.svc.cluster.local:7946 # :7946 is the default memberlist port.

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

compactor:
  working_directory: /loki/compactor
  shared_store: s3
  compaction_interval: 5m
```


## 3-S3-Without-Credentials-Snippet.yaml

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


## 4-BOS-Example.yaml

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


## 5-S3-And-DynamoDB-Snippet.yaml

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


## 6-Cassandra-Snippet.yaml

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


## 7-Schema-Migration-Snippet.yaml

```yaml

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


## 8-GCS-Snippet.yaml

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


## 9-Expanded-S3-Snippet.yaml

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


## 10-S3-And-DynamoDB-With-KMS-Snippet.yaml

```yaml

# This partial configuration uses S3 for chunk storage and uses DynamoDB for index storage and a KMS CMK for encryption

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
    sse_encryption: true
    sse:
      type: SSE-KMS
      kms_key_id: 1234abcd-12ab-34cd-56ef-1234567890ab
    dynamodb:
      dynamodb_url: dynamodb://access_key:secret_access_key@region
      kms_key_id: 0987dcba-09fe-87dc-65ba-ab0987654321
```


## 11-COS-HMAC-Example.yaml

```yaml

# This partial configuration uses IBM Cloud Object Storage (COS) for chunk storage. HMAC will be used for authenticating with COS.

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
    region: <region>
    access_key_id: <access_key_id>
    secret_access_key: <secret_access_key>

```


## 12-COS-APIKey-Example.yaml

```yaml

# This partial configuration uses IBM Cloud Object Storage (COS) for chunk storage. APIKey will be used for authenticating with COS.

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
    region: <region>
    api_key: <api_key_to_authenticate_with_cos>
    service_instance_id: <cos_service_instance_id>
    auth_endpoint: <iam_endpoint_for_authentication>

```


## 13-COS-Trusted-Profile-Example.yaml

```yaml

# This partial configuration uses IBM Cloud Object Storage (COS) for chunk storage. 
# A trusted profile will be used for authenticating with COS. We can either pass
# the trusted profile name or trusted profile ID along with the compute resource token file.
# If we pass both trusted profile name and trusted profile ID it should be of 
# the same trusted profile.
# In order to use trusted profile authentication we need to follow an additional step to create a trusted profile.
# For more details about creating a trusted profile, see https://cloud.ibm.com/docs/account?topic=account-create-trusted-profile&interface=ui.

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
    region: <region>
    auth_endpoint: <iam_endpoint_for_authentication>
    cr_token_file_path: <path_to_compute_resource_token>
    trusted_profile_name: <name_of_the_trusted_profile> # You can also use trusted_profile_id instead of trusted_profile_name

```

