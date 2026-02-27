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
       - from: "2024-04-01"
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
connection string is either blank or malformed. The expected connection string should contain key value pairs separated by semicolons. For example 'DefaultEndpointsProtocol=https;AccountName=<accountName>;AccountKey=<accountKey>;EndpointSuffix=core.windows.net'
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
unrecognized named filesystem storage config <name>
unrecognized named swift storage config <name>
```

Or for an unrecognized store type:

```text
unrecognized named storage type: <storeType>
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

Cache errors occur when Loki cannot connect to or communicate with caching backends (Memcached, Redis).

### Error: Redis client setup failed

**Error message:**

```text
redis client setup failed: <details>
```

**Cause:**

Loki cannot establish a connection to the Redis server. Common causes include:

- Incorrect Redis endpoint
- Network connectivity issues
- Authentication failures
- TLS configuration problems

**Resolution:**

1. **Verify Redis connectivity** from the Loki host:

   ```bash
   redis-cli -h <REDIS-HOST> -p <REDIS-PORT> ping
   ```

1. **Check the Redis endpoint configuration**:

   ```yaml
   chunk_store_config:
     chunk_cache_config:
       redis:
         endpoint: redis:6379
         timeout: 500ms
   ```

1. **Configure authentication** if required:

   ```yaml
   chunk_store_config:
     chunk_cache_config:
       redis:
         endpoint: redis:6379
         password: ${REDIS_PASSWORD}
   ```

**Properties:**

- Enforced by: Cache client initialization
- Retryable: Yes (with correct configuration)
- HTTP status: N/A (startup failure or degraded operation)
- Configurable per tenant: No

### Error: Could not lookup Redis host

**Error message:**

```text
could not lookup host: <hostname>
```

**Cause:**

DNS resolution failed for the Redis hostname.

**Resolution:**

1. **Verify DNS resolution**:

   ```bash
   nslookup redis-host
   ```

1. **Use an IP address** if DNS is not available:

   ```yaml
   chunk_store_config:
     chunk_cache_config:
       redis:
         endpoint: 10.0.0.100:6379
   ```

1. **Check your DNS configuration** and network settings.

**Properties:**

- Enforced by: DNS resolution
- Retryable: Yes
- HTTP status: N/A
- Configurable per tenant: No

### Error: Unexpected Redis PING response

**Error message:**

```text
redis: Unexpected PING response "<response>"
```

**Cause:**

The Redis server returned an unexpected response to a PING command. This could indicate:

- The endpoint is not a Redis server
- A proxy or load balancer is interfering
- Redis is in an error state

**Resolution:**

1. **Verify the endpoint** is actually a Redis server.
1. **Check Redis health**:

   ```bash
   redis-cli -h <HOST> -p <PORT> INFO
   ```

1. **Review proxy configurations** if using a load balancer in front of Redis.

**Properties:**

- Enforced by: Redis health check
- Retryable: Yes
- HTTP status: N/A
- Configurable per tenant: No

### Error: Multiple cache systems not supported

**Error message:**

```text
use of multiple cache storage systems is not supported
```

**Cause:**

Both Memcached and Redis cache backends are configured for the same cache type. Only one caching backend is supported per cache type.

**Resolution:**

1. **Choose one cache backend** per cache type:

   ```yaml
   chunk_store_config:
     chunk_cache_config:
       # Use either memcached OR redis, not both
       redis:
         endpoint: redis:6379
       memcached: {}  # Remove this
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: No cache configured

**Error message:**

```text
no cache configured
```

**Cause:**

A results cache is required for the query frontend but no cache configuration was provided.

**Resolution:**

1. **Configure a cache backend**:

   ```yaml
   query_range:
     results_cache:
       cache:
         memcached:
           expiration: 1h
         memcached_client:
           addresses: memcached:11211
   ```

1. **Or disable results caching** if not needed:

   ```yaml
   query_range:
     cache_results: false
   ```

**Properties:**

- Enforced by: Query frontend initialization
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## Ring and cluster communication errors

Ring errors occur when Loki components cannot properly communicate through the [hash ring](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/hash-rings/), which is used to distribute work across instances. The ring is fundamental to Loki's distributed operation.

### Error: Too many unhealthy instances in the ring

**Error message:**

```text
too many unhealthy instances in the ring
```

**Cause:**

The ring contains too many unhealthy instances to satisfy the replication factor. For example, with a replication factor of 3, at least 3 healthy instances must be available.

**Resolution:**

1. **Check the health of ring members**:
    Open a browser and navigate to http://localhost:3100/ring. You should see the Loki ring page.

    OR

   ```bash
   curl -s http://loki:3100/ring | jq '.shards[] | select(.state != "ACTIVE")'
   ```

1. **Restart unhealthy instances** that are stuck in a bad state.
1. **Scale up instances** if there aren't enough healthy members.
1. **Check resource constraints** (CPU, memory, disk) on unhealthy instances.

**Properties:**

- Enforced by: Ring replication
- Retryable: Yes (after instances recover)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Empty ring

**Error message:**

```text
empty ring
```

**Cause:**

No instances are registered in the ring. This typically occurs during initial cluster startup, for example if your ingesters are OOM crashing, or due to misconfiguration.

**Resolution:**

1. **Wait for instances to register** during initial startup.
1. **Check ingesters** to make sure they are running.
1. **Check that all instances can communicate** over the configured ports.
1. **Verify ring configuration** across all components, especially memberlist configuration:

   ```yaml
   ingester:
     lifecycler:
       ring:
         kvstore:
           store: memberlist
         replication_factor: 3
   ```

1. **Check KV store health** (Consul, etcd, or memberlist):

   ```bash
   # For memberlist
   curl -s http://loki:3100/memberlist
   ```

**Properties:**

- Enforced by: Ring operations
- Retryable: Yes (after instances register)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Instance not found in the ring

**Error message:**

```text
instance <id> not found in the ring
```

**Cause:**

A specific instance is expected to be in the ring but isn't registered. This can happen after a restart if the instance hasn't re-joined the ring yet.

**Resolution:**

1. **Wait for the instance to re-register** in the ring.
1. **Check the instance's logs** for ring join failures.
1. **Verify KV store connectivity** from the instance.

**Properties:**

- Enforced by: Ring operations
- Retryable: Yes (after instance registers)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Instance owns no tokens

**Error message:**

```text
this instance owns no tokens
```

**Cause:**

The instance has joined the ring but hasn't claimed any tokens. Without tokens, the instance cannot receive any work. This can happen if:

- The instance is still starting up
- Token claim failed
- The KV store update didn't propagate

**Resolution:**

1. **Wait for token assignment** during startup.
1. **Check the ring status** for the instance:

   ```bash
   curl -s http://loki:3100/ring
   ```

1. **Restart the instance** if tokens are not assigned after startup completes.
1. **Check KV store connectivity** and health.

**Properties:**

- Enforced by: Lifecycler readiness check
- Retryable: Yes (after tokens are assigned)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Error talking to the KV store

**Error message:**

```text
error talking to the KV store
```

**Cause:**

The instance cannot communicate with the key-value store used for ring state. The KV store (Consul, etcd, or memberlist) is required for ring coordination.

**Resolution:**

1. **Check KV store health and connectivity**:

   ```bash
   # For Consul
   curl http://consul:8500/v1/status/leader
   
   # For etcd
   etcdctl endpoint health
   ```

1. **Verify network connectivity** between Loki instances and the KV store.
1. **Check firewall rules** allow traffic on KV store ports.
1. **For memberlist**, verify that gossip ports are accessible between all instances:

   ```yaml
   memberlist:
     bind_port: 7946
     join_members:
       - loki-memberlist:7946
   ```

**Properties:**

- Enforced by: Ring lifecycler
- Retryable: Yes (after KV store recovery)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: No ring returned from the KV store

**Error message:**

```text
no ring returned from the KV store
```

**Cause:**

The KV store responded but returned an empty or invalid ring descriptor. This can happen if the KV store was recently initialized or its data was cleared.

**Resolution:**

1. **Wait for ring initialization** during first startup.
1. **Check if the KV store data was accidentally cleared**.
1. **Restart all ring members** to re-register if the KV store was reset.

**Properties:**

- Enforced by: Ring lifecycler
- Retryable: Yes (after ring initialization)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Failed to join memberlist cluster

**Error message:**

```text
failed to join memberlist cluster on startup
```

Or:

```text
joining memberlist cluster failed
```

**Cause:**

The instance could not join the memberlist gossip cluster. Common causes:

- Seed nodes are unreachable
- DNS resolution failure for join addresses
- Firewall blocking gossip ports
- All existing members are down

**Resolution:**

1. **Check that join members are reachable**:

   ```bash
   # Test connectivity to seed nodes
   nc -zv loki-memberlist 7946
   ```

1. **Verify DNS resolution** for join addresses:

   ```bash
   nslookup loki-memberlist
   ```

1. **Check memberlist configuration**:

   ```yaml
   memberlist:
     bind_port: 7946
     join_members:
       - loki-gossip-ring.loki.svc.cluster.local:7946
   ```

1. **Ensure firewall rules** allow UDP and TCP traffic on the gossip port (default 7946).
1. **For Kubernetes**, verify that the headless service for memberlist is configured correctly.

**Properties:**

- Enforced by: Memberlist KV client
- Retryable: Yes (automatic retries with backoff)
- HTTP status: N/A (startup failure or degraded operation)
- Configurable per tenant: No

### Error: Re-joining memberlist cluster failed

**Error message:**

```text
re-joining memberlist cluster failed
```

**Cause:**

After being disconnected from the memberlist cluster, the instance failed to rejoin. This can happen during network partitions or after prolonged network issues.

**Resolution:**

1. **Check network connectivity** between cluster members.
1. **Verify other cluster members are healthy**.
1. **Restart the affected instance** if automatic rejoin continues to fail.
1. **Review network stability** frequent re-joins indicate underlying network issues.

**Properties:**

- Enforced by: Memberlist KV client
- Retryable: Yes (automatic retries)
- HTTP status: N/A (degraded operation)
- Configurable per tenant: No

## Component readiness errors

Readiness errors occur when Loki components are not ready to serve requests. These errors are returned by the [`/ready` health check endpoint](http://localhost:3100/ready) and prevent load balancers from routing traffic to unready instances.

### Error: Application is stopping

**Error message:**

```text
Application is stopping
```

**Cause:**

Loki is shutting down and no longer accepting new requests. This is normal during graceful shutdown.

**Resolution:**

1. **Wait for the instance to restart** if this is a rolling update.
1. **Check if the shutdown is expected** (maintenance, scaling down).
1. **Review orchestrator logs** (Kubernetes, systemd) if the shutdown is unexpected.

**Properties:**

- Enforced by: Loki readiness handler
- Retryable: Yes (after restart)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Some services are not running

**Error message:**

```text
Some services are not Running:
<state>: <count>
<state>: <count>
```

For example:

```text
Some services are not Running:
Starting: 1
Failed: 2
```

**Cause:**

One or more internal Loki services have failed to start or have stopped unexpectedly. The error message lists each service state with a count of services in that state.

**Resolution:**

1. **Check Loki logs** for errors from the listed services.
1. **Verify configuration** for the affected services.
1. **Check resource availability** (memory, disk, CPU).
1. **Restart the instance** if services are stuck.

**Properties:**

- Enforced by: Loki service manager
- Retryable: Yes (after services recover)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Ingester not ready

**Error message:**

```text
Ingester not ready: <details>
```

When the ingester's own state check fails, `<details>` contains the ingester state, giving the full message:

```text
Ingester not ready: ingester not ready: <state>
```

Where `<state>` is the service state, for example `Starting`, `Stopping`, or `Failed`.

**Cause:**

The ingester is not in a ready state to accept writes or serve reads. The detail message indicates the specific reason, such as:

- The ingester is still starting up and joining the ring (`Starting`)
- The lifecycler is not ready (lifecycler error text)
- The ingester is waiting for minimum ready duration after ring join

**Resolution:**

1. **Wait for startup to complete** - ingesters take time to join the ring and become ready.
1. **Check ring membership**:

   ```bash
   curl -s http://ingester:3100/ring
   ```

1. **Review logs** for startup errors.
1. **Adjust the minimum ready duration** if startup is too slow:

   ```yaml
   ingester:
     lifecycler:
       min_ready_duration: 15s
   ```

**Properties:**

- Enforced by: Ingester readiness check
- Retryable: Yes (after ingester becomes ready)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: No queriers connected to query frontend

**Error message:**

```text
Query Frontend not ready: not ready: number of queriers connected to query-frontend is 0
```

**Cause:**

The query frontend has no querier workers connected. Without queriers, the frontend cannot process any queries. This typically occurs when:

- Queriers are not yet started
- Queriers cannot reach the frontend
- gRPC connectivity issues between queriers and frontend

**Resolution:**

1. **Check that queriers are running** and healthy.
1. **Verify querier configuration** points to the correct frontend address:

   ```yaml
   frontend_worker:
     frontend_address: query-frontend:9095
   ```

1. **Check gRPC connectivity** between queriers and the frontend:

   ```bash
   # Test gRPC port connectivity
   nc -zv query-frontend 9095
   ```

1. **Review querier logs** for connection errors.

**Properties:**

- Enforced by: Query frontend (v1) readiness check
- Retryable: Yes (after queriers connect)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: No schedulers connected to frontend worker

**Error message:**

```text
Query Frontend not ready: not ready: number of schedulers this worker is connected to is 0
```

**Cause:**

The query frontend worker has no active connections to any query scheduler. This prevents the frontend from dispatching queries.

**Resolution:**

1. **Check that query schedulers are running** and healthy.
1. **Verify scheduler address configuration**:

   ```yaml
   frontend_worker:
     scheduler_address: query-scheduler:9095
   ```

1. **Check gRPC connectivity** between the frontend and schedulers.
1. **Review query scheduler logs** for errors.

**Properties:**

- Enforced by: Query frontend (v2) readiness check
- Retryable: Yes (after schedulers connect)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

## gRPC and message size errors

<!-- Additional content in next PRs.  Just leaving the headings here for context and so that I can keep things in order if PRs merge out of sequence. -->

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

