---
title: Migrate to Thanos storage clients
menuTitle: Migrate to Thanos storage clients
description: Migration guide for moving from existing storage clients to Thanos storage clients.
weight: 
---
# Migrate to Thanos storage clients

Loki release 3.4 introduces new object storage clients based on the [Thanos Object Storage Client Go module](https://github.com/thanos-io/objstore).

One of the reasons for making this change is to have a consistent storage configuration across Grafana Loki, Mimir and other telemetry databases from Grafana Labs. If you are already using Grafana Mimir or Pyroscope, you can reuse the storage configuration for setting up Loki.

This is an opt-in feature with the Loki 3.4 release. In a future release, Thanos will become the default way of configuring storage and the existing storage clients will be deprecated.

{{< admonition type="note" >}}
The new storage configuration deviates from the existing format. The following sections describe the changes in detail for each provider.
Refer to the [Thanos storage configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) to view the complete list of supported storage providers and their configuration options.
{{< /admonition >}}

### Enable the new storage clients

1. Enable Thanos storage clients by setting `use_thanos_objstore` to `true` in the `storage_config` section or by setting the `-use-thanos-objstore` flag to true. When enabled, configuration under `storage_config.object_store` takes effect instead of existing storage configurations.

   ```yaml
   # Uses the new storage clients for connecting to gcs backend
   storage_config:
     use_thanos_objstore: true # enable the new storage clients
     object_store:
       gcs:
         bucket_name: "example-bucket"
   ```

1. As an alternative, you can also configure the new clients in the common `storage` section if you prefer to use the `common` config section.

   ```yaml
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
   common:
     storage:
       object_store:
         gcs:
           bucket_name: "example-bucket"
   ```

1. Ruler storage should be configured under the `ruler_storage` section when using the new storage clients.

   ```yaml
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
   ruler_storage:
      backend: gcs
      gcs:
         bucket_name: "example-bucket"
   ```

1. If you are using `store.object-prefix` flag or the corresponding `object_prefix` YAML setting, you'll need to update your configuration to use the new `object_store.storage-prefix` flag or the corresponding `storage_prefix` YAML setting.

   ```yaml
   # Example configuration to prefix all objects with "prefix"
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
      object_store:
         storage_prefix: "prefix"
   ```

### GCS Storage Migration

When migrating from the existing [Google Cloud Storage (GCS)](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#gcs_storage_config) storage client to the new Thanos-based client, you'll need to update your configuration parameters as follows:

{{< responsive-table >}}
| Existing Parameter    | New Parameter         | Required Changes                                                                                             |
|---------------------|-----------------------|--------------------------------------------------------------------------------------------------------------|
| `bucket_name`       | `bucket_name`         | No changes required                                                                                          |
| `service_account`   | `service_account`     | No changes required                                                                                          |
| `chunk_buffer_size` | `chunk_buffer_size`   | No changes required                                                                                          |
| `enable_retries`    | `max_retries`         | Replace `enable_retries` (bool) with `max_retries` (int). Set a value > 1 to enable retries, or 1 to disable |
| `request_timeout`   | Removed              | Remove parameter                                                                                             |
| `enable_opencensus` | Removed              | Remove parameter                                                                                             |
| `enable_http2`      | Removed              | Remove parameter                                                                                             |
{{< /responsive-table >}}

**Example configuration migration (GCS):**

_**Existing configuration:**_

```yaml
storage_config:
  gcs:
    bucket_name: example-bucket
    chunk_buffer_size: 10MB
    enable_retries: true
```

_**New configuration**_ (Thanos-based):

```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    gcs:
      bucket_name: example-bucket
      chunk_buffer_size: 10MB
      max_retries: 5
```

### Amazon S3 Storage Migration

When migrating from the existing [Amazon S3](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#aws_storage_config) storage client to the new Thanos-based client, update or remove parameters as follows:

{{< responsive-table >}}
| Existing Parameter    | New Parameter         | Required Changes                                                                                                                   |
|---------------------|-----------------------|------------------------------------------------------------------------------------------------------------------------------------|
| `bucket_names`      | `bucket_name`         | Rename this parameter. If you previously used multiple buckets, you must consolidate to a single bucket (Thanos supports only one). |
| `endpoint`          | `endpoint`            | No changes required.                                                                                                               |
| `region`            | `region`              | No changes required.                                                                                                               |
| `access_key_id`     | `access_key_id`       | No changes required.                                                                                                               |
| `secret_access_key` | `secret_access_key`   | No changes required.                                                                                                               |
| `session_token`     | `session_token`       | No changes required.                                                                                                               |
| `insecure`          | `insecure`            | No changes required.                                                                                                               |
| `disable_dualstack` | `dualstack_enabled`   | Renamed and inverted. If you had `disable_dualstack: false`, set `dualstack_enabled: true`.                                         |
| `storage_class`     | `storage_class`       | No changes required.                                                                                                               |
| `s3`                | Removed               | Remove or replace with `endpoint` if you used the URL-based setup.                                                                 |
| `S3ForcePathStyle`  | Removed or replaced   | If you need path-based addressing, set `bucket_lookup_type: path` in the new config. Otherwise, remove it.                         |
| `signature_version` | Removed               | Remove parameter. Thanos always uses Signature Version 4 (V4).                                                                     |
| `http_config`      | `http`       | Move subfields (such as timeouts, CA file, etc.) into the `http:` block in the Thanos configuration. |
| `sse`              | `sse`        | Migrate any SSE settings (e.g., `type`, `kms_key_id`) into the `sse:` block in the Thanos configuration. |
| `backoff_config`   | `max_retries`| Replace the advanced backoff settings with a single integer (`max_retries`). Set to 1 to disable retries, or a higher value to enable them. |
{{< /responsive-table >}}

**Example configuration migration (S3):**

_**Existing configuration**_

```yaml
storage_config:
  aws:
    bucket_names: my-bucket1,my-bucket2   # multiple buckets no longer supported
    endpoint: s3.amazonaws.com
    region: us-west-2
    access_key_id: example-key
    secret_access_key: example-secret
    signature_version: v4
    disable_dualstack: true
    storage_class: STANDARD
    http_config:
      timeout: 1m
      insecure_skip_verify: false
    # ...
    backoff_config:
      max_retries: 5
    sse:
      type: SSE-KMS
      kms_key_id: mySSEKey
```

_**New configuration** (Thanos-based)_

```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    s3:
      bucket_name: my-bucket1                       # single bucket
      endpoint: s3.amazonaws.com
      region: us-west-2
      access_key_id: example-key
      secret_access_key: example-secret
      dualstack_enabled: false                        # was disable_dualstack: true
      storage_class: STANDARD
      max_retries: 5
      http:
        insecure_skip_verify: false
      sse:
        type: SSE-KMS
        kms_key_id: mySSEKey
```

For more advanced configuration options (such as `list_objects_version`, `bucket_lookup_type`, etc.), see the [Thanos S3 configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config).

### Azure Storage Migration

When migrating from the existing [Azure](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#azure_storage_config) storage client to the new Thanos-based client, no changes are required if you are using the following parameters:

{{< responsive-table >}}
| Existing Parameter | New Parameter | Required Changes |
|-----------------|---------------|------------------|
| `account_name` | `account_name` | No changes required |
| `account_key` | `account_key` | No changes required |
| `container_name` | `container_name` | No changes required |
| `endpoint_suffix` | `endpoint_suffix` | No changes required |
| `user_assigned_id` | `user_assigned_id` | No changes required |
| `connection_string` | `connection_string` | No changes required |
| `max_retries` | `max_retries` | No changes required |
| `chunk_delimiter` | `chunk_delimiter` | No changes required |
{{< /responsive-table >}}

If you are using an authentication method other than storage account key or user-assigned managed identity, you'll have to pass the neccessary credetials using environment variables.
For more details, refer to [Azure Identity Client Module for Go](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity).

### Filesystem Storage Migration

When migrating from the existing [Filesystem storage](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#local_storage_config)
client to the new Thanos-based client, update or remove parameters as follows:

{{< responsive-table >}}
| Existing Parameter | New Parameter | Required Changes              |
|-----------------|---------------|-------------------------------|
| `directory`     | `dir`         | Rename `directory` to `dir`.  |
{{< /responsive-table >}}

**Example configuration migration (Filesystem):**

_**Existing configuration** (`FSConfig`)_

```yaml
storage_config:
  filesystem:
    directory: /var/loki/chunks
```

_**New configuration** (Thanos-based)_

```yaml
storage_config:
  use_thanos_objstore: true
  object_store:
    filesystem:
      dir: /var/loki/chunks
```

{{< admonition type="note" >}}
For providers not listed here, refer to the [Thanos storage configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config).
{{< /admonition >}}