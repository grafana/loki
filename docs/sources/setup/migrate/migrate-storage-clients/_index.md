---
title: Migrate to Thanos storage clients
menuTitle: Migrate to Thanos storage clients
description: Migration guide for moving from legacy storage clients to Thanos storage clients.
weight: 
---
# Migrate to Thanos storage clients

Loki release 3.4 introduces new object storage clients based on the [Thanos Object Storage Client Go module](https://github.com/thanos-io/objstore).

One of the reasons for making this change is to have a consistent storage configuration across LGTM+ stack. If you are already using Grafana Mimir or Pyroscope, you can reuse the storage configuration for setting up Loki.

This is an opt-in feature with the Loki 3.4 release. In a future release, Thanos will become the default way of configuring storage and the existing storage clients will be deprecated.

The new storage configuration deviates from the existing format. The following sections describe the changes in detail for each provider.
Refer to the [Thanos storage configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) to view the complete list of supported storage providers and their configuration options.

### Enable the new storage clients

1. Enable Thanos storage clients by setting `use_thanos_objstore` to `true` in the `storage_config` section or by setting the `-use-thanos-objstore` flag to true. When enabled, configuration under `storage_config.object_store` takes effect instead of legacy storage configurations.

   ```yaml
   # Uses the new storage clients for connecting to gcs backend
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
   object_store:
      gcs: 
         bucket: "example-bucket"
   ```
1. As an alternative, you can also configure the new clients in the common storage section if you prefer to use the common config section.
   ```yaml
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
   common:
      storage:
         object_store:
            gcs:
            bucket: "example-bucket"
   ```
1. Ruler storage should be configured under the `ruler_storage` section when using the new storage clients.
   ```yaml
   storage_config:
      use_thanos_objstore: true # enable the new storage clients
   ruler_storage:
      backend: gcs
      gcs:
         bucket: "example-bucket"
   ```

{{< admonition type="note" >}}
Following sections document the steps for migrating the clients. Only some of the commonly used parameters are listed here.
For a complete list of supported parameters, refer to the [Thanos storage configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#thanos_object_store_config) and compare that to what you are using in your setup.
{{< /admonition >}}

### GCS Storage Migration

When migrating from the legacy [Google Cloud Storage (GCS)](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#gcs_storage_config) storage client to the new Thanos-based client, you'll need to update your configuration parameters as follows:

{{< responsive-table >}}
| Legacy Parameter | New Parameter | Required Changes |
|-----------------|---------------|------------------|
| `bucket_name` | `bucket_name` | No changes required |
| `chunk_buffer_size` | `chunk_size_bytes` | Rename parameter to `chunk_size_bytes` |
| `enable_retries` | `enable_retries` | No changes required |
{{< /responsive-table >}}

**Example configuration migration:**

Legacy configuration:
```yaml
storage_config:
  gcs:
    bucket_name: example-bucket
    chunk_buffer_size: 10MB
    enable_retries: true
```

New configuration:
```yaml
storage_config:
  use_thanos_objstore: true
object_store:
  gcs:
    bucket_name: example-bucket
    chunk_size_bytes: 10MB
    enable_retries: true
```

### Amazon S3 Storage Migration

When migrating from the legacy [Amazon S3](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#aws_storage_config) storage client to the new Thanos-based client, you'll need to update your configuration parameters as follows:

{{< responsive-table >}}
| Legacy Parameter | New Parameter | Required Changes |
|-----------------|---------------|------------------|
| `endpoint` | `endpoint` | No changes required |
| `region` | `region` | No changes required |
| `bucket_names` | `bucket_name` | Rename parameter to `bucket_name` |
| `access_key_id` | `access_key_id` | No changes required |
| `secret_access_key` | `secret_access_key` | No changes required |
| `session_token` | `session_token` | No changes required |
| `insecure` | `insecure` | No changes required |
| `disable_dualstack` | `dualstack_enabled` | Rename parameter to `dualstack_enabled` and invert logic |
| `storage_class` | `storage_class` | No changes required |
{{< /responsive-table >}}

**Example configuration migration:**

Legacy configuration:
```yaml
storage_config:
  aws:
    endpoint: s3.amazonaws.com
    region: us-west-2
    bucket_names: example-bucket
    access_key_id: example-key
    secret_access_key: example-secret
    insecure: false
    disable_dualstack: true
    storage_class: STANDARD
```

New configuration:
```yaml
storage_config:
  use_thanos_objstore: true
object_store:
  s3:
    endpoint: s3.amazonaws.com
    region: us-west-2
    bucket_name: example-bucket
    access_key_id: example-key
    secret_access_key: example-secret
    insecure: false
    dualstack_enabled: false
    storage_class: STANDARD
```

### Azure Storage Migration

When migrating from the legacy [Azure](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#azure_storage_config) storage client to the new Thanos-based client, no changes are required if you are using the following parameters:

{{< responsive-table >}}
| Legacy Parameter | New Parameter | Required Changes |
|-----------------|---------------|------------------|
| `account_name` | `account_name` | No changes required |
| `account_key` | `account_key` | No changes required |
| `container_name` | `container_name` | No changes required |
| `endpoint_suffix` | `endpoint_suffix` | No changes required |
| `user_assigned_id` | `user_assigned_id` | No changes required |
| `connection_string` | `connection_string` | No changes required |
| `max_retries` | `max_retries` | No changes required |
{{< /responsive-table >}}

If you are using an authentication method other than storage account key or user-assigned managed identity, you'll have to pass the neccessary credetials using environment variables.
For more details, refer to [Azure Identity Client Module for Go](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity).
