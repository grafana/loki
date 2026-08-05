---
title: "Configuration examples for using Thanos-based storage clients"
menuTitle: Thanos storage examples
description: "Real-world examples for using Thanos-based S3, GCS, Azure, MinIO, and filesystem clients in Grafana Loki."
weight: 100
---

# Configuration examples for using Thanos-based storage clients

Use these examples as a starting point for configuring [Thanos based object storage clients](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) in Grafana Loki. Each example is a complete configuration you can adapt and run, not just a snippet.

## GCS example

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
  path_prefix: /loki

schema_config:
  configs:
    - from: 2020-07-01
      store: tsdb
      object_store: gcs
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  use_thanos_objstore: true
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
    cache_ttl: 24h # Can be increased for faster performance over longer query periods, uses more disk space
  object_store:
    gcs:
      bucket_name: <BUCKET_NAME>

      # JSON either from a Google Developers Console client_credentials.json file,
      # or a Google Developers service account key. Needs to be valid JSON, not a
      # filesystem path. If empty, fallback to Google default logic:
      # 1. A JSON file whose path is specified by the GOOGLE_APPLICATION_CREDENTIALS
      # environment variable. For workload identity federation, refer to
      # https://cloud.google.com/iam/docs/how-to#using-workload-identity-federation
      # on how to generate the JSON configuration file for on-prem/non-Google cloud
      # platforms.
      # 2. A JSON file in a location known to the gcloud command-line tool:
      # $HOME/.config/gcloud/application_default_credentials.json.
      # 3. On Google Compute Engine it fetches credentials from the metadata server.
      service_account:  |-
        {
            "type": "service_account",
            "project_id": "project",
            "private_key_id": "abcdefghijklmnopqrstuvwxyz12345678906666",
            "private_key": "-----BEGIN PRIVATE KEY-----\...\n-----END PRIVATE KEY-----\n",
            "client_email": "project@example.iam.gserviceaccount.com",
            "client_id": "123456789012345678901",
            "auth_uri": "https://accounts.google.com/o/oauth2/auth",
            "token_uri": "https://oauth2.googleapis.com/token",
            "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
            "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/project@example.iam.gserviceaccount.com"
        }
```

## S3 example

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
  path_prefix: /loki

schema_config:
  configs:
    - from: 2020-07-01
      store: tsdb
      object_store: s3
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  use_thanos_objstore: true
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
    cache_ttl: 24h
  object_store:
    s3:
      bucket_name: <BUCKET_NAME>
      endpoint: <ENDPOINT>
      region: <REGION>
      # You can either declare the access key and secret in the config or
      # use environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY which will be picked up by the AWS SDK.
      access_key_id: <ACCESS_KEY_ID>
      secret_access_key: <SECRET_ACCESS_KEY>
```

## Azure example

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
  path_prefix: /loki

schema_config:
  configs:
    - from: 2020-07-01
      store: tsdb
      object_store: azure
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  use_thanos_objstore: true
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
    cache_ttl: 24h
  object_store:
    azure:
      account_name: <ACCOUNT_NAME>
      account_key: ${SECRET_ACCESS_KEY} # loki expands environment variables
      container_name: <CONTAINER_NAME>
```

## MinIO / S3-compatible example

MinIO and other S3-compatible object stores use the same `object_store.s3` client as AWS S3. Set `bucket_lookup_type: path` (MinIO's equivalent of the legacy `s3forcepathstyle` option), and `insecure: true` if MinIO is served over plain HTTP:

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
  path_prefix: /loki

schema_config:
  configs:
    - from: 2020-07-01
      store: tsdb
      object_store: s3
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  use_thanos_objstore: true
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
    cache_ttl: 24h
  object_store:
    s3:
      bucket_name: <BUCKET_NAME>
      # Use a fully qualified domain name (fqdn), like localhost, without a scheme.
      endpoint: <FQDN>:<PORT>
      access_key_id: <ACCESS_KEY_ID>
      secret_access_key: <SECRET_ACCESS_KEY>
      insecure: true
      bucket_lookup_type: path
```

## Filesystem example

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
    - from: 2020-07-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

storage_config:
  use_thanos_objstore: true
  object_store:
    filesystem:
      dir: /tmp/loki/chunks
```
