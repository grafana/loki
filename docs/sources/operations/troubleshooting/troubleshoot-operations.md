---
title: Troubleshoot Loki operations
menuTitle: Troubleshoot operations
description: Describes how to troubleshoot and debug specific errors related to Loki configuration, storage, networking, and cluster operations.
weight: 100
---

# Troubleshoot Loki operations

This guide helps you troubleshoot errors that occur during Loki operations, including configuration issues, storage backend problems, cluster communication failures, and service component errors. These errors are distinct from ingestion (write path) and query (read path) errors covered in separate troubleshooting topics.

Before you begin, ensure you have the following:

- Access to Loki logs and metrics
- Permissions to view and modify Loki configuration
- Understanding of your deployment topology (single binary/monolithic, simple scalable, microservices/distributed)

## Configuration errors

Configuration errors occur during Loki startup or when loading runtime configuration. These errors prevent Loki from starting or operating correctly.

### Error: Multiple config errors found

**Error message:**

```text
MULTIPLE CONFIG ERRORS FOUND, PLEASE READ CAREFULLY
<list of configuration errors>
```

**Cause:**

Multiple configuration validation errors were detected during startup. Loki aggregates all configuration errors rather than failing on the first one.

**Resolution:**

1. **Review all listed errors** carefully - each error message describes a specific configuration problem.
1. **Check your configuration file** for syntax errors and invalid values.
1. **Validate your configuration** before applying:

   ```bash
   loki -config.file=/path/to/config.yaml -verify-config
   ```

**Properties:**

- Enforced by: Loki startup
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Too many storage configs

**Error message:**

```text
too many storage configs provided in the common config, please only define one storage backend
```

**Cause:**

Multiple storage backends are configured in the common configuration section. Loki requires a single storage backend for the common config.

**Resolution:**

1. **Use only one storage backend** in your common config:

   ```yaml
   common:
     storage:
       # Choose only ONE of the following:
       s3:
         endpoint: s3.amazonaws.com
         bucketnames: loki-data
       # OR
       gcs:
         bucket_name: loki-data
       # OR
       azure:
         container_name: loki-data
   ```

1. **For multiple storage backends**, configure them explicitly in specific sections rather than common config.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Persist tokens path prefix required

**Error message:**

```text
if persist_tokens is true, path_prefix MUST be defined
```

**Cause:**

The `persist_tokens` option is enabled for a ring but no `path_prefix` is specified. Loki needs a path to store the token file.

**Resolution:**

1. **Set the path prefix**:

   ```yaml
   common:
     path_prefix: /var/loki
     persist_tokens: true
   
   ingester:
     lifecycler:
       ring:
         kvstore:
           store: memberlist
       tokens_file_path: /var/loki/tokens
   ```

1. **Or disable persist_tokens** if you don't need token persistence:

```yaml
   common:
     persist_tokens: false
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Conflicting gRPC client configs

**Error message:**

```text
both `grpc_client_config` and (`query_frontend_grpc_client` or `query_scheduler_grpc_client`) are set at the same time. Please use only `query_frontend_grpc_client` and `query_scheduler_grpc_client`
```

**Cause:**

Both the deprecated `grpc_client_config` and the newer specific gRPC client configs are set. These are mutually exclusive.

**Resolution:**

1. **Remove the deprecated config** and use specific gRPC client configs:

   ```yaml
   # Remove this:
   # grpc_client_config: ...
   
   # Use these instead:
   query_frontend_grpc_client:
     max_recv_msg_size: 104857600
   query_scheduler_grpc_client:
     max_recv_msg_size: 104857600
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Schema v13 required for structured metadata

**Error message:**

```text
CONFIG ERROR: schema v13 is required to store Structured Metadata and use native OTLP ingestion, your schema version is <version>. Set `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false` and restart Loki. Then proceed to update to schema v13 or newer before re-enabling this config, search for 'Storage Schema' in the docs for the schema update procedure
```

**Cause:**

Structured metadata is enabled but the active schema version is older than v13. Structured metadata requires schema v13 or newer.

**Resolution:**

1. **Disable structured metadata temporarily**:

   ```yaml
   limits_config:
     allow_structured_metadata: false
   ```

1. **Update your schema config** to v13 or newer:

   ```yaml
   schema_config:
     configs:
       - from: "2024-01-01"
         store: tsdb
         object_store: s3
         schema: v13
         index:
           prefix: index_
           period: 24h
   ```

1. **Re-enable structured metadata** after the schema migration is complete.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: TSDB index type required for structured metadata

**Error message:**

```text
CONFIG ERROR: `tsdb` index type is required to store Structured Metadata and use native OTLP ingestion, your index type is `<type>` (defined in the `store` parameter of the schema_config). Set `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false` and restart Loki. Then proceed to update the schema to use index type `tsdb` before re-enabling this config, search for 'Storage Schema' in the docs for the schema update procedure
```

**Cause:**

Structured metadata is enabled but the active index type is not TSDB. Structured metadata requires the TSDB index type.

**Resolution:**

1. **Disable structured metadata temporarily** and migrate to the TSDB index type:

   ```yaml
   limits_config:
     allow_structured_metadata: false
   
   schema_config:
     configs:
       - from: "2024-01-01"
         store: tsdb
         object_store: s3
         schema: v13
   ```

1. **Re-enable structured metadata** after migrating to TSDB.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: TSDB directories not configured

**Error message:**

```text
CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `active_index_directory` is not set, please set this directly or set `path_prefix:` in the `common:` section
```

Or:

```text
CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `cache_location` is not set, please set this directly or set `path_prefix:` in the `common:` section
```

**Cause:**

The TSDB index type is configured in the schema but required local directories for index files are not set.

**Resolution:**

1. **Set the common path prefix** (simplest approach):

   ```yaml
   common:
     path_prefix: /var/loki
   ```

1. **Or configure directories explicitly**:

   ```yaml
   storage_config:
     tsdb_shipper:
       active_index_directory: /var/loki/tsdb-index
       cache_location: /var/loki/tsdb-cache
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Compactor working directory empty

**Error message:**

```text
CONFIG ERROR: `compactor:` `working_directory:` is empty, please set a valid directory or set `path_prefix:` in the `common:` section
```

**Cause:**

The compactor requires a working directory for index compaction, but none is configured.

**Resolution:**

1. **Set the common path prefix**:

   ```yaml
   common:
     path_prefix: /var/loki
   ```

1. **Or set the compactor working directory explicitly**:

   ```yaml
   compactor:
     working_directory: /var/loki/compactor
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Index cache validity conflict

**Error message:**

```text
CONFIG ERROR: the active index is <type> which is configured to use an `index_cache_validity` (TTL) of <duration>, however the chunk_retain_period is <duration> which is LESS than the `index_cache_validity`. This can lead to query gaps, please configure the `chunk_retain_period` to be greater than the `index_cache_validity`
```

**Cause:**

The chunk retain period is shorter than the index cache validity (TTL), which can cause query gaps where data exists in the index cache but the chunks have already been flushed and removed from ingesters.

**Resolution:**

1. **Increase the chunk retain period** to be greater than the index cache validity:

   ```yaml
   ingester:
     chunk_retain_period: 15m  # Must be > index_cache_validity
   
   storage_config:
     index_cache_validity: 5m
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid target with legacy read mode

**Error message:**

```text
CONFIG ERROR: invalid target, cannot run backend target with legacy read mode
```

**Cause:**

The `backend` target is configured while legacy read mode is enabled. These are incompatible deployment configurations.

**Resolution:**

1. **Disable legacy read mode** if using the `backend` target:

   ```yaml
   # Remove or set to false:
   legacy_read_mode: false
   ```

1. **Or use a different target** compatible with legacy read mode.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Unrecognized index or store type

**Error message:**

```text
unrecognized `store` (index) type `<type>`, choose one of: <supported_types>
```

Or:

```text
unrecognized `object_store` type `<type>`, which also does not match any named_stores. Choose one of: <supported_types>. Or choose a named_store
```

**Cause:**

The schema configuration references an index type or object store type that Loki does not recognize.

**Resolution:**

1. **Use a supported index type**: `tsdb` (recommended) or `boltdb-shipper`

1. **Use a supported object store type**: `s3`, `gcs`, `azure`, `swift`, `filesystem`, `bos`

1. **Or reference a valid named store** defined in your configuration:

   ```yaml
   storage_config:
     named_stores:
       aws:
         my-store:
           endpoint: s3.amazonaws.com
           bucketnames: my-bucket
   
   schema_config:
     configs:
       - from: 2024-01-01
         store: tsdb
         object_store: my-store  # References the named store
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Overrides exporter requires runtime configuration

**Error message:**

```text
overrides-exporter has been enabled, but no runtime configuration file was configured
```

**Cause:**

The overrides-exporter target is enabled but no runtime configuration file is provided. The overrides-exporter needs a runtime config to expose tenant-specific limit overrides as metrics.

**Resolution:**

1. **Configure a runtime configuration file**:

   ```yaml
   runtime_config:
     file: /etc/loki/runtime-config.yaml
   ```

1. **Or disable the overrides-exporter** if not needed by removing it from your target list.

**Properties:**

- Enforced by: Module initialization
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid override for tenant

**Error message:**

```text
invalid override for tenant <tenant>: <details>
```

**Cause:**

The runtime configuration file contains an invalid override for a specific tenant. The override failed validation.

**Resolution:**

1. **Review the runtime config** file for the specified tenant.
1. **Validate the override values** against the limits configuration schema.
1. **Fix invalid values** such as negative durations, invalid label matchers, or out-of-range settings.

**Properties:**

- Enforced by: Runtime configuration loader
- Retryable: No (runtime config must be fixed)
- HTTP status: N/A (runtime config reload failure)
- Configurable per tenant: Yes

### Error: Retention period too short

**Error message:**

```text
retention period must be >= 24h was <duration>
```

**Cause:**

A stream-level retention rule specifies a retention period shorter than 24 hours, which is the minimum allowed.

**Resolution:**

1. **Set retention periods to at least 24 hours**:

   ```yaml
   limits_config:
     retention_stream:
       - selector: '{namespace="dev"}'
         priority: 1
         period: 24h  # Must be >= 24h
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: Yes

### Error: Invalid query store max look back period

**Error message:**

```text
it is an error to specify a non zero `query_store_max_look_back_period` value when using any object store other than `filesystem`
```

**Cause:**

The `query_store_max_look_back_period` is set to a non-zero value with a storage backend other than `filesystem`. This setting only applies to local filesystem storage.

**Resolution:**

1. **Remove the setting** if using object storage:

   ```yaml
   # Remove or set to 0:
   query_store_max_look_back_period: 0
   ```

1. **Or use filesystem storage** if this setting is needed for local development.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## Authentication and tenant errors

Authentication and tenant errors occur when requests are missing required tenant identification or when tenant IDs are invalid. In multi-tenant mode, every request must include a valid tenant ID.

### Error: No org ID

**Error message:**

```text
no org id
```

**Cause:**

A request was made to Loki without the required `X-Scope-OrgID` header. In multi-tenant mode, every request must identify the tenant.

**Resolution:**

1. **Add the `X-Scope-OrgID` header** to your requests:

   ```bash
   curl -H "X-Scope-OrgID: my-tenant" http://loki:3100/loki/api/v1/push ...
   ```

1. **For Grafana**, configure the tenant ID in the Loki data source settings under "HTTP Headers".

1. **For Alloy**, set the tenant ID in the `loki.write` component:

   ```alloy
   loki.write "default" {
     endpoint {
       url       = "http://loki:3100/loki/api/v1/push"
       tenant_id = "my-tenant"
     }
   }
   ```

1. **Disable multi-tenancy** for single-tenant deployments:

   ```yaml
   auth_enabled: false
   ```

**Properties:**

- Enforced by: Authentication middleware
- Retryable: Yes (with tenant ID)
- HTTP status: 401 Unauthorized
- Configurable per tenant: No

### Error: Multiple org IDs present

**Error message:**

```text
multiple org IDs present
```

**Cause:**

The request contains multiple different tenant IDs, but the operation requires a single tenant. This can happen when a request is forwarded through multiple proxies that each inject a tenant ID.

**Resolution:**

1. **Ensure only one tenant ID** is set in the `X-Scope-OrgID` header.
1. **Check proxy configurations** for conflicting tenant ID injection.
1. **For cross-tenant queries**, use pipe-separated tenant IDs only where supported:

   ```bash
   curl -H "X-Scope-OrgID: tenant1|tenant2" http://loki:3100/loki/api/v1/query ...
   ```

**Properties:**

- Enforced by: Tenant resolver
- Retryable: Yes (with correct tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Tenant ID too long

**Error message:**

```text
tenant ID is too long: max 150 characters
```

**Cause:**

The tenant ID exceeds the maximum allowed length of 150 characters.

**Resolution:**

1. **Use a shorter tenant ID** (maximum 150 characters).

**Properties:**

- Enforced by: Tenant validation
- Retryable: Yes (with valid tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Unsafe tenant ID

**Error message:**

```text
tenant ID is '.' or '..'
```

**Cause:**

The tenant ID is set to `.` or `..`, which are reserved filesystem path components and could cause path traversal issues.

**Resolution:**

1. **Choose a different tenant ID** that is not `.` or `..`.

**Properties:**

- Enforced by: Tenant validation
- Retryable: Yes (with valid tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Tenant ID contains unsupported character

**Error message:**

```text
tenant ID '<id>' contains unsupported character '<char>'
```

**Cause:**

The tenant ID contains characters that are not allowed. Tenant IDs must consist of alphanumeric characters, hyphens, underscores, and periods.

**Resolution:**

1. **Use only supported characters** in your tenant ID: letters, numbers, hyphens (`-`), underscores (`_`), and periods (`.`).

**Properties:**

- Enforced by: Tenant validation
- Retryable: Yes (with valid tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Deletion not available for tenant

**Error message:**

```text
deletion is not available for this tenant
```

**Cause:**

A delete request was submitted for a tenant that does not have deletion enabled. Log deletion must be explicitly enabled per tenant.

**Resolution:**

1. **Enable deletion for the tenant** in the runtime configuration:

   ```yaml
   overrides:
     my-tenant:
       deletion_mode: filter-and-delete  # Or "filter-only"
   ```

   Valid deletion modes:
   - `disabled` - Deletion is not allowed (default)
   - `filter-only` - Lines matching delete requests are filtered at query time but not physically deleted
   - `filter-and-delete` - Lines are filtered at query time and physically deleted during compaction

1. **Ensure the compactor is configured** for retention:

   ```yaml
   compactor:
     retention_enabled: true
     delete_request_store: s3
   ```

**Properties:**

- Enforced by: Compactor deletion handler
- Retryable: No (configuration must change)
- HTTP status: 403 Forbidden
- Configurable per tenant: Yes


## Storage backend errors

Storage backend errors occur when Loki cannot communicate with or properly configure object storage (Amazon S3, Google Cloud Services, Microsoft Azure, Swift, or filesystem).

### Error: Unsupported storage backend

**Error message:**

```text
unsupported storage backend
```

**Cause:**

The specified storage backend type is not recognized. This typically occurs when a typo exists in the storage type configuration.

**Resolution:**

1. **Use a valid storage backend type**:

   - `s3` - Amazon S3 or S3-compatible storage
   - `gcs` - Google Cloud Storage
   - `azure` - Azure Blob Storage
   - `swift` - OpenStack Swift
   - `filesystem` - Local filesystem
   - `bos` - Baidu Object Storage

   ```yaml
   storage_config:
     boltdb_shipper:
       shared_store: s3  # Must be one of the valid types
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid characters in storage prefix

**Error message:**

```text
storage prefix contains invalid characters, it may only contain digits, English alphabet letters and dashes
```

**Cause:**

The storage path prefix contains invalid characters. Only alphanumeric characters and dashes are allowed.

**Resolution:**

1. **Use valid characters** in your storage prefix:

   ```yaml
   storage_config:
     # Invalid: prefix_with_underscore_or/special chars
     # Valid: my-loki-data or lokilogs123
     aws:
       s3: s3://my-bucket/my-loki-data
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Unsupported S3 SSE type

**Error message:**

```text
unsupported S3 SSE type
```

**Cause:**

The S3 server-side encryption (SSE) type is not supported. Loki supports specific SSE types.

**Resolution:**

1. **Use a supported SSE type**:

   ```yaml
   storage_config:
     aws:
       sse:
         type: SSE-S3    # Or SSE-KMS
   ```

   Supported types:
   - `SSE-S3` - Server-side encryption with Amazon S3-managed keys
   - `SSE-KMS` - Server-side encryption with AWS KMS-managed keys

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid S3 SSE encryption context

**Error message:**

```text
invalid S3 SSE encryption context
```

**Cause:**

The SSE-KMS encryption context is malformed and cannot be parsed as valid JSON.

**Resolution:**

1. **Provide valid JSON** for the encryption context:

   ```yaml
   storage_config:
     aws:
       sse:
         type: SSE-KMS
         kms_key_id: alias/my-key
         kms_encryption_context: '{"key": "value"}'  # Valid JSON
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid S3 endpoint prefix

**Error message:**

```text
the endpoint must not prefixed with the bucket name
```

**Cause:**

The S3 endpoint incorrectly includes the bucket name as a prefix. This can cause path-style vs virtual-hosted-style URL issues.

**Resolution:**

1. **Remove the bucket name** from the endpoint and configure it separately:

   ```yaml
   storage_config:
     aws:
       # Incorrect:
       # endpoint: my-bucket.s3.amazonaws.com
       
       # Correct:
       endpoint: s3.amazonaws.com
       bucketnames: my-bucket
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid STS endpoint

**Error message:**

```text
sts-endpoint must be a valid url
```

**Cause:**

The AWS STS (Security Token Service) endpoint URL is malformed or invalid.

**Resolution:**

1. **Provide a valid URL** for the STS endpoint:

   ```yaml
   storage_config:
     aws:
       sts_endpoint: https://sts.us-east-1.amazonaws.com
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Azure connection string malformed

**Error message:**

```text
connection string is either blank or malformed. The expected connection string should contain key value pairs separated by semicolons
```

**Cause:**

The Azure storage connection string is missing or doesn't follow the expected format.

**Resolution:**

1. **Use a valid connection string format**:

   ```yaml
   storage_config:
     azure:
       # Use account credentials:
       account_name: myaccount
       account_key: mykey
       
       # Or connection string:
       connection_string: "DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=mykey;EndpointSuffix=core.windows.net"
   ```

1. **Verify the connection string** in Azure Portal under Storage Account > Access Keys.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Unrecognized named storage config

**Error message:**

```text
unrecognized named storage config <name>
```

Or for specific backends:

```text
unrecognized named s3 storage config <name>
unrecognized named gcs storage config <name>
unrecognized named azure storage config <name>
```

**Cause:**

A named storage configuration referenced in the schema config doesn't exist in the named stores configuration.

**Resolution:**

1. **Define the named store** in your configuration:

   ```yaml
   storage_config:
     named_stores:
       aws:
         my-s3-store:  # This name must match the reference
           endpoint: s3.amazonaws.com
           bucketnames: my-bucket
   
   schema_config:
     configs:
       - from: 2024-01-01
         store: tsdb
         object_store: my-s3-store  # References the named store above
   ```

1. **Check spelling** of the store name in both the definition and reference.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## Cache errors

<!-- Additional content in next PRs.  Just leaving the headings here for context and so that I can keep things in order if PRs merge out of sequence. -->

## Ring and cluster communication errors



## Component readiness errors



## gRPC and message size errors



## TLS and certificate errors



## DNS resolution errors



## Scheduler and frontend errors



## Index gateway errors



## Compactor and retention errors



## Ruler errors



## Kafka integration errors



## Bloom gateway errors



## Write-ahead log (WAL) errors



## Ingester lifecycle errors



## Pattern ingester errors



## API parameter errors

