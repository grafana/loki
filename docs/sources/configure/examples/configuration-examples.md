---
title: Configuration
description: Loki Configuration Examples and Snippets
weight:  100
---
# Configuration

## 1-Local-Configuration-Example.yaml

```yaml

# This is a complete configuration to deploy Loki backed by the filesystem.
# The index will be shipped to the storage via tsdb-shipper.

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
    store: tsdb
    object_store: filesystem
    schema: v13
    index:
      prefix: index_
      period: 24h

storage_config:
  filesystem:
    directory: /tmp/loki/chunks

```


## 2-S3-Cluster-Example.yaml

```yaml

# This is a complete configuration to deploy Loki backed by a s3-compatible API
# like MinIO for storage.
# Index files will be written locally at /loki/index and, eventually, will be shipped to the storage via tsdb-shipper.

auth_enabled: false

server:
  http_listen_port: 3100

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory
  replication_factor: 1
  path_prefix: /loki

schema_config:
  configs:
  - from: 2020-05-15
    store: tsdb
    object_store: s3
    schema: v13
    index:
      prefix: index_
      period: 24h

storage_config:
 tsdb_shipper:
   active_index_directory: /loki/index
   cache_location: /loki/index_cache
 aws:
   s3: s3://access_key:secret_access_key@custom_endpoint/bucket_name
   s3forcepathstyle: true

```


## 3-S3-Without-Credentials-Snippet.yaml

```yaml

# If you don't wish to hard-code S3 credentials you can also configure an EC2
# instance role by changing the `storage_config` section.

storage_config:
  aws:
    s3: s3://region/bucket_name
      

```


## 4-GCS-Example.yaml

```yaml

# This is a complete configuration to deploy Loki backed by a GCS.
# Index files will be written locally at /loki/index and, eventually, will be shipped to the storage via tsdb-shipper.

auth_enabled: false

server:
  http_listen_port: 3100

common:
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory
  replication_factor: 1
  path_prefix: /loki

schema_config:
  configs:
  - from: 2020-05-15
    store: tsdb
    object_store: gcs
    schema: v13
    index:
      prefix: index_
      period: 24h

storage_config:
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
  gcs:
    bucket_name: replace_by_your_bucked_name

```


## 5-BOS-Example.yaml

```yaml

# This is a partial configuration to deploy Loki backed by Baidu Object Storage (BOS).
# The index will be shipped to the storage via tsdb-shipper.
common:
  path_prefix: /tmp/loki

schema_config:
  configs:
    - from: 2020-05-15
      store: tsdb
      object_store: bos
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  bos:
    bucket_name: bucket_name_1
    endpoint: bj.bcebos.com
    access_key_id: access_key_id
    secret_access_key: secret_access_key

```


## 6-Compactor-Snippet.yaml

```yaml

# This partial configuration sets the compactor to use S3 and run the compaction every 5 minutes.
# Downloaded index files for compaction are stored in /loki/compactor.

compactor:
  working_directory: /tmp/loki/compactor
  compaction_interval: 5m

```


## 7-Schema-Migration-Snippet.yaml

```yaml

common:
  path_prefix: /tmp/loki

schema_config:
  configs:
    # Starting from 2018-04-15 Loki should store indexes on BoltDB with the v11 schema
    # using daily periodic tables and chunks on filesystem.
    # The index tables will be prefixed with "index_".
  - from: "2018-04-15"
    store: boltdb-shipper
    object_store: filesystem
    schema: v11
    index:
        period: 24h
        prefix: index_

  # Starting from 2023-6-15 Loki should store indexes on TSDB with the v13 schema
  # using daily periodic tables and chunks on AWS S3.
  - from: "2023-06-15"
    store: tsdb
    object_store: s3
    schema: v13
    index:
        period: 24h
        prefix: index_

```


## 8-alibaba-cloud-storage-Snippet.yaml

```yaml

# This partial configuration uses Alibaba for chunk storage.
common:
  path_prefix: /tmp/loki

schema_config:
  configs:
  - from: 2020-05-15
    store: tsdb
    object_store: alibabacloud
    schema: v13
    index:
      prefix: index_
      period: 24h

storage_config:
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
  alibabacloud:
    bucket: <bucket>
    endpoint: <endpoint>
    access_key_id: <access_key_id>
    secret_access_key: <secret_access_key>

```


## 9-S3-With-SSE-KMS-Snippet.yaml

```yaml

# This partial configuration uses S3 for chunk storage and a KMS CMK for encryption.

storage_config:
  aws:
    s3: s3://access_key:secret_access_key@region/bucket_name
    sse:
      type: SSE-KMS
      kms_key_id: 1234abcd-12ab-34cd-56ef-1234567890ab

```


## 10-Expanded-S3-Snippet.yaml

```yaml

# S3 configuration supports an expanded configuration.
# Either an `s3` endpoint URL can be used, or an expanded configuration can be used.

storage_config:
  aws:
    bucketnames: bucket_name1, bucket_name2
    endpoint: s3.endpoint.com
    region: s3_region
    access_key_id: s3_access_key_id
    secret_access_key: s3_secret_access_key
    insecure: false
    http_config:
      idle_conn_timeout: 90s
      response_header_timeout: 0s
      insecure_skip_verify: false
    s3forcepathstyle: true
    

```


## 11-COS-HMAC-Example.yaml

```yaml

# This partial configuration uses IBM Cloud Object Storage (COS) for chunk storage. HMAC will be used for authenticating with COS.
common:
  path_prefix: /tmp/loki

schema_config:
  configs:
    - from: 2020-10-01
      store: tsdb
      object_store: cos
      schema: v13
      index:
        period: 24h
        prefix: index_

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
common:
  path_prefix: /tmp/loki

schema_config:
  configs:
    - from: 2020-10-01
      store: tsdb
      object_store: cos
      schema: v13
      index:
        period: 24h
        prefix: index_

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
common:
  path_prefix: /tmp/loki

schema_config:
  configs:
    - from: 2020-10-01
      store: tsdb
      object_store: cos
      schema: v13
      index:
        period: 24h
        prefix: index_

storage_config:
  cos:
    bucketnames: <bucket1, bucket2>
    endpoint: <endpoint>
    region: <region>
    auth_endpoint: <iam_endpoint_for_authentication>
    cr_token_file_path: <path_to_compute_resource_token>
    trusted_profile_name: <name_of_the_trusted_profile> # You can also use trusted_profile_id instead of trusted_profile_name

```


## 15-Memberlist-Ring-Snippet.yaml

```yaml

# This partial configuration uses memberlist for the ring.

common:
  ring:
    kvstore:
      store: memberlist
  replication_factor: 1
  path_prefix: /loki

memberlist:
  join_members:
    # You can use a headless k8s service for all distributor, ingester and querier components.
    - loki-gossip-ring.loki.svc.cluster.local:7946 # :7946 is the default memberlist port.

```


## 16-Azure-Account-Name-Example.yaml

```yaml

# This partial configuration uses Azure for chunk storage
storage_config:
  azure:
    # For the account-key, see docs: https://docs.microsoft.com/en-us/azure/storage/common/storage-account-keys-manage?tabs=azure-portal
    account_key: <azure_blob_access_key>
    account_name: <azure_account_name>
    container_name: <azure_storage_bucket_name>
    use_managed_identity: false
    # Providing a user assigned ID will override use_managed_identity
    #user_assigned_id: <user-assigned-identity-id>
    request_timeout: 0
    # Configure `endpoint_suffix` if you are using private azure cloud like azure stack hub and will use this endpoint suffix to compose container and blob storage URL. Ex: https://account_name.endpoint_suffix/container_name/blob_name
    #endpoint_suffix: <endpoint-suffix>
    # If `connection_string` is set, the `account_name` and `endpoint_suffix` values will not be used. Use this method over `account_key` if you need to authenticate via an SAS token. Or if you use the Azurite emulator.
    #connection_string: <connection-string>

```


## 17-Azure-Service-Principal-Example.yaml

```yaml

# This partial configuration uses Azure for chunk storage and a service principal for authentication
storage_config:
  azure:
    use_service_principal: true
    # Azure tenant ID used to authenticate through Azure OAuth
    tenant_id: <tenant-id>
    # Azure Service Principal ID
    client_id: <client-id>
    # Azure Service Principal secret key
    client_secret: <client-secret>
    # See https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blobs-introduction#containers
    container_name: <azure_storage_bucket_name>
    request_timeout: 0

```

