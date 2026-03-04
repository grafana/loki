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
   Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

   OR

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
   Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

   OR

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

gRPC errors occur during inter-component communication. Loki components communicate using gRPC for ring coordination, query execution, and data transfer.

### Error: Message size too large

**Error message:**

```text
message size too large than max (<size> vs <max>)
```

Or for the decompressed body:

```text
decompressed message size too large than max (<size> vs <max>)
```

**Cause:**

The compressed or decompressed body of an HTTP push request to the distributor exceeds the configured limit.

**Default configuration:**

- `distributor.max_recv_msg_size`: 100MB (compressed request body limit)
- `distributor.max_decompressed_size`: 5000MB (decompressed body limit, defaults to 50× `max_recv_msg_size`)

**Resolution:**

1. **Increase the distributor receive message size limit**:

   ```yaml
   distributor:
     max_recv_msg_size: 209715200      # 200MB compressed
     max_decompressed_size: 10737418240  # 10GB decompressed
   ```

1. **Reduce push batch sizes** in your log shipping client (Alloy, Promtail, etc.) to send smaller individual requests.

1. **Reduce the amount of data per request** by lowering the batch size or flush interval in your client.

**Properties:**

- Enforced by: Distributor push handler
- Retryable: No (request must be smaller or limits increased)
- HTTP status: 413 Request Entity Too Large (compressed), 400 Bad Request (decompressed)
- Configurable per tenant: No

### Error: Response larger than max message size

**Error message:**

```text
response larger than the max message size (<size> vs <max>)
```

**Cause:**

A query result from the querier to the frontend exceeds the maximum allowed gRPC response size. This typically happens with queries that return very large result sets.

**Default configuration:**

- `server.grpc_server_max_send_msg_size`: 4MB (gRPC server send limit on the querier)
- `querier.query_frontend_grpc_client.max_recv_msg_size`: 100MB (gRPC client receive limit on the querier worker)

**Resolution:**

1. **Reduce query scope** to return fewer results:
   - Add more specific label matchers
   - Reduce the time range
   - Lower the entries limit

1. **Increase gRPC message size limits** if needed. Apply these settings to querier nodes:

   ```yaml
   server:
     grpc_server_max_send_msg_size: 209715200   # 200MB

   querier:
     query_frontend_grpc_client:
       max_recv_msg_size: 209715200             # 200MB
   ```

**Properties:**

- Enforced by: Querier worker
- Retryable: No (query scope or limits must change)
- HTTP status: 413 Request Entity Too Large
- Configurable per tenant: No

### Error: Compressed message size exceeds limit

**Error message:**

```text
compressed message size <size> exceeds limit <limit>
```

**Cause:**

The compressed body of an HTTP push request exceeds the distributor's configured limit. This check runs after the request body has been fully read and validates the total compressed size against the configured maximum.

**Default configuration:**

- `distributor.max_recv_msg_size`: 100MB

**Resolution:**

1. **Reduce batch sizes** in your log shipping client.
1. **Split large batches** into smaller, more frequent requests.
1. **Increase the limit** if needed:

   ```yaml
   distributor:
     max_recv_msg_size: 209715200   # 200MB
   ```

**Properties:**

- Enforced by: Distributor push handler
- Retryable: No (request must be smaller)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

## TLS and certificate errors

TLS errors occur when Loki or its clients cannot establish secure connections due to certificate issues.

### Error: TLS certificate loading failed

**Error message:**

```text
error loading ca cert: <path>
```

Or:

```text
error loading client cert: <path>
```

Or:

```text
error loading client key: <path>
```

Or:

```text
failed to load TLS certificate <cert_path>,<key_path>
```

**Cause:**

Loki cannot load TLS certificates from the specified paths. Common causes:

- Certificate files don't exist at the configured paths
- Permission issues prevent reading the files
- Certificate or key format is invalid
- Certificate and key don't match

**Resolution:**

1. **Verify certificate files exist** and are readable:

   ```bash
   ls -la /path/to/cert.pem /path/to/key.pem /path/to/ca.pem
   ```

1. **Check file permissions** (the Loki process must be able to read them).

1. **Validate the certificate format**:

   ```bash
   openssl x509 -in /path/to/cert.pem -noout -text
   openssl rsa -in /path/to/key.pem -check
   ```

1. **Verify cert and key match**:

   ```bash
   openssl x509 -noout -modulus -in cert.pem | md5sum
   openssl rsa -noout -modulus -in key.pem | md5sum
   # Both should produce the same hash
   ```

1. **Check your TLS configuration**:

   ```yaml
   server:
     http_tls_config:
       cert_file: /path/to/cert.pem
       key_file: /path/to/key.pem
       client_ca_file: /path/to/ca.pem
     grpc_tls_config:
       cert_file: /path/to/cert.pem
       key_file: /path/to/key.pem
       client_ca_file: /path/to/ca.pem
   ```

**Properties:**

- Enforced by: TLS configuration
- Retryable: No (certificates must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: TLS configuration error

**Error message:**

```text
error generating http tls config: <details>
```

Or:

```text
error generating grpc tls config: <details>
```

Where `<details>` may include messages such as `TLS version %q not recognized`, `cipher suite %q not recognized`, or `unknown TLS version: <version>`.

**Cause:**

The TLS configuration is invalid. This can happen when:

- An unsupported TLS version string is supplied
- Cipher suite configuration is invalid
- Client auth type is unrecognized

**Resolution:**

1. **Review TLS settings** for compatibility issues.
1. **Use supported TLS versions** by setting `tls_min_version` at the top level of the `server` block:

   ```yaml
   server:
     tls_min_version: VersionTLS12
   ```

   Valid values are `VersionTLS10`, `VersionTLS11`, `VersionTLS12`, and `VersionTLS13`. There is no `max_version` setting; `tls_min_version` is the only version constraint.

1. **Check cipher suite configuration** if customized.

**Properties:**

- Enforced by: TLS initialization
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## DNS resolution errors

DNS errors occur when Loki cannot resolve hostnames for service discovery or backend connections.

### Error: DNS lookup timeout

**Error message:**

```text
msg="failed to resolve server addresses" err="... DNS lookup timeout: [<address>] ..."
```

**Cause:**

DNS resolution exceeded the 5-second timeout when trying to resolve addresses for Loki service discovery or backend connections.
This error is emitted by the index gateway and bloom gateway DNS discovery loops.
The `DNS lookup timeout: [<address>]` string is the context cause embedded within the `err` field; the full address list is formatted as a Go slice (for example, `[dns+loki-index-gateway.loki.svc.cluster.local:9095]`).

**Resolution:**

1. **Check DNS server availability** and configuration.
1. **Verify hostname resolution**:

   ```bash
   nslookup <hostname>
   dig <hostname>
   ```

1. **Use IP addresses** as a workaround if DNS is unreliable:

   ```yaml
   # Instead of dns+hostname:port
   memberlist:
     join_members:
       - 10.0.0.1:7946
       - 10.0.0.2:7946
   ```

1. **For Kubernetes**, ensure CoreDNS is healthy and headless services are configured correctly.

**Properties:**

- Enforced by: Index gateway client, bloom gateway client DNS discovery loop
- Retryable: Yes (DNS may recover)
- HTTP status: N/A (connectivity failure)
- Configurable per tenant: No

## Scheduler and frontend errors

These errors relate to query scheduling, frontend workers, and queue management.

### Error: Scheduler is not running

**Error message:**

```text
scheduler is not running
```

**Cause:**

The query scheduler service is not in a running state. This can occur when:

- The scheduler is starting up
- The scheduler encountered a fatal error
- The scheduler is shutting down

**Resolution:**

1. **Check scheduler logs** for startup errors or crashes.
1. **Verify scheduler health**:

   ```bash
   curl -s http://scheduler:3100/ready
   ```

1. **Check scheduler ring membership** if using ring-based scheduling:

   ```bash
   curl -s http://scheduler:3100/ring | jq
   ```

**Properties:**

- Enforced by: Scheduler service
- Retryable: Yes (wait for scheduler to become ready)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Too many outstanding requests

**Error message:**

```text
too many outstanding requests
```

**Cause:**

The query queue has reached its maximum capacity. This indicates the system is overloaded with queries.

**Resolution:**

1. **Scale out queriers** to process queries faster:

   ```yaml
   querier:
     max_concurrent: 10
   ```

1. **Increase queue capacity** (with caution). The default is `32000`; increase beyond that only if you have confirmed the system can handle the additional load.  Note that increasing the queue is often necessary because of how many subqueries can be generated by large values for `tsdb_max_query_parallelism`. Generally it's preferable to add more queriers and leave this setting unchanged.

   ```yaml
   query_scheduler:
     max_outstanding_requests_per_tenant: 64000
   ```

1. **Rate limit queries** at the client or load balancer level.
1. **Optimize slow queries** to reduce queue time.

**Properties:**

- Enforced by: Query scheduler/frontend
- Retryable: Yes
- HTTP status: 429 Too Many Requests
- Configurable per tenant: No

### Error: Querying is disabled

**Error message:**

```text
querying is disabled, please contact your Loki operator
```

**Cause:**

Query parallelism has been set to zero, effectively disabling all queries. This is typically done intentionally during maintenance.

**Resolution:**

1. **Check the relevant parallelism setting for your index type.** For TSDB indexes (the current default), `tsdb_max_query_parallelism` supersedes `max_query_parallelism`. Either value being set to zero triggers this error. Verify that both are greater than zero:

   ```yaml
   limits_config:
     max_query_parallelism: 32          # default; applies to non-TSDB schemas
     tsdb_max_query_parallelism: 128    # default; applies to TSDB schemas
   ```

1. **Size `tsdb_max_query_parallelism` to your ingest volume.** Typical values in production are in the range of 128–2048, proportional to the volume of logs ingested per day:

   | Daily ingest volume | Typical value |
   |---|---|
   | Low–moderate | 128–256 |
   | High | 512 |
   | Tens of TB/day | 1024–2048 |

1. **Account for the querier capacity this requires.** Each unit of parallelism consumes one querier worker slot. With the default `querier.max_concurrent` of `4`, the number of queriers needed to fully parallelize a single query is:

   ```
   queriers needed = tsdb_max_query_parallelism / max_concurrent
   ```

   For example, `tsdb_max_query_parallelism: 2048` with `max_concurrent: 4` requires 512 queriers to run one query fully in parallel. Production deployments supporting many tenants running large queries simultaneously commonly run thousands of queriers.

1. **Contact your administrator** if you don't have access to change these settings.

**Properties:**

- Enforced by: Query frontend
- Retryable: No (configuration must change)
- HTTP status: 429 Too Many Requests
- Configurable per tenant: Yes

### Error: No frontend address

**Error message:**

```text
no frontend address
```

**Cause:**

The scheduler received a request from a frontend but no frontend address was provided for sending responses back.

**Resolution:**

1. **Check frontend configuration** to ensure the address is set:

   ```yaml
   frontend:
     address: query-frontend:9095
   ```

1. **Verify gRPC connectivity** between frontend and scheduler.

**Properties:**

- Enforced by: Scheduler
- Retryable: No (configuration issue)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Scheduler shutting down

**Error message:**

```text
scheduler is shutting down
```

**Cause:**

The frontend scheduler worker detected that the scheduler is in shutdown mode and cannot accept new requests.

**Resolution:**

1. **Wait for shutdown to complete** and the scheduler to restart.
1. **Check if this is expected** (rolling update, maintenance).
1. **Retry the request** after the scheduler is healthy.

**Properties:**

- Enforced by: Scheduler
- Retryable: Yes (after scheduler restart)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

## Index gateway errors

Index gateway errors occur when queriers cannot communicate with index gateways for index lookups.

### Error: Index gateway unhealthy in ring

**Error message:**

```text
index-gateway is unhealthy in the ring
```

**Cause:**

The index gateway instance detects itself as unhealthy in the ring and refuses to process queries. This is a self-check: before handling tenant requests, the gateway verifies it appears in the set of healthy ring members.

**Resolution:**

1. **Check index gateway health**:

   ```bash
   curl -s http://index-gateway:3100/ready
   ```

1. **View the ring status**:
   Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

   OR

   ```bash
   curl -s http://index-gateway:3100/ring
   ```

1. **Check logs** for errors preventing the gateway from becoming healthy.
1. **Restart the index gateway** if it's stuck in an unhealthy state.

**Properties:**

- Enforced by: Index gateway ring
- Retryable: Yes (wait for gateway to become healthy)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: No index gateway instances found

**Error message:**

```text
no index gateway instances found for tenant <tenant>
```

**Cause:**

No index gateway instances are available in the ring to serve the tenant's request. This could be due to:

- All index gateways are unhealthy
- Shuffle sharding excludes this tenant
- Ring is empty

**Resolution:**

1. **Check if any index gateways are running**:

   ```bash
   curl -s http://index-gateway:3100/ring | jq '.shards | length'
   ```

1. **Verify ring mode is configured** if using shuffle sharding. The index gateway must run in `ring` mode and the per-tenant shard size must be set:

   ```yaml
   index_gateway:
     mode: ring

   limits_config:
     index_gateway_shard_size: 3  # default = 0 (use all instances)
   ```

1. **Scale up index gateways** if needed.

**Properties:**

- Enforced by: Index gateway client
- Retryable: Yes
- HTTP status: 500 Internal Server Error
- Configurable per tenant: Yes (via `index_gateway_shard_size` in `limits_config`)

### Error: Index client not initialized

**Error message:**

```text
index client is not initialized likely due to boltdb-shipper not being used
```

**Cause:**

The index gateway was queried for operations that require the index client, but the client wasn't initialized because the boltdb-shipper store isn't configured.

**Resolution:**

1. **Verify your schema config** uses the correct index store:

   ```yaml
   schema_config:
     configs:
       - from: 2024-01-01
         store: tsdb
         object_store: s3
         schema: v13
         index:
           prefix: index_
           period: 24h
   ```

1. **Check if the operation requires boltdb-shipper** - some legacy operations may not be supported with TSDB.

**Properties:**

- Enforced by: Index gateway
- Retryable: No (configuration/schema issue)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No 

## Compactor and retention errors

<<<<<<< HEAD
<!-- Additional content in next PRs.  Just leaving the headings here for context and so that I can keep things in order if PRs merge out of sequence. -->
=======
Compactor errors occur during index compaction or retention enforcement.

### Error: No chunks found in table

**Error message:**

```text
no chunks found in table, please check if there are really no chunks and manually drop the table or see if there is a bug causing us to drop whole index table
```

**Cause:**

The compactor found an empty index table during retention processing. This could indicate:

- All chunks in the table have expired
- The table was never populated
- Data corruption

**Resolution:**

1. **Verify the table should be empty**:

   ```bash
   # Check if data exists for the time period
   logcli query '{job=~".+"}' --from="<table-start-time>" --to="<table-end-time>" --limit=1
   ```

1. **If the table is legitimately empty**, manually delete it from object storage.
1. **If data should exist**, investigate potential data loss.

**Properties:**

- Enforced by: Compactor retention
- Retryable: No (requires manual intervention)
- HTTP status: N/A (background process)
- Configurable per tenant: No

### Error: Delete request store not configured

**Error message:**

```text
compactor.delete-request-store should be configured when retention is enabled
```

**Cause:**

Retention is enabled but no store is configured for tracking delete requests.

**Resolution:**

1. **Configure the delete request store**:

   ```yaml
   compactor:
     retention_enabled: true
     delete_request_store: s3
   ```

1. **Or disable retention** if not needed:

   ```yaml
   compactor:
     retention_enabled: false
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Max compaction parallelism invalid

**Error message:**

```text
max compaction parallelism must be >= 1
```

**Cause:**

The compactor's parallelism setting is configured to zero or a negative number.

**Resolution:**

1. **Set a valid parallelism value**:

   ```yaml
   compactor:
     max_compaction_parallelism: 1  # Must be >= 1
   ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Delete request not found

**Error message:**

```text
could not find delete request with given id
```

**Cause:**

An attempt to cancel a delete request failed because no matching request exists.

**Resolution:**

1. **List existing delete requests**:

   ```bash
   curl -s http://compactor:3100/loki/api/v1/delete | jq
   ```

1. **Verify the delete request ID** is correct.
1. **Check if the request has already been processed** and removed.

**Properties:**

- Enforced by: Compactor API
- Retryable: No
- HTTP status: 404 Not Found
- Configurable per tenant: No

### Error: Retention is not enabled

**Error message:**

```text
Retention is not enabled
```

**Cause:**

A delete request was submitted but retention is not enabled in the compactor configuration. Delete requests require retention to be enabled.

**Resolution:**

1. **Enable retention** in the compactor:

   ```yaml
   compactor:
     retention_enabled: true
     delete_request_store: s3
   ```

1. **Restart the compactor** after changing the configuration.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No (configuration must change)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Invalid delete request time format

**Error message:**

```text
invalid start time: require unix seconds or RFC3339 format
```

Or:

```text
invalid end time: require unix seconds or RFC3339 format
```

**Cause:**

The start or end time in a delete request is not in a valid format.

**Resolution:**

1. **Use Unix seconds or RFC3339 format**:

   ```bash
   # Unix seconds
   curl -X POST http://compactor:3100/loki/api/v1/delete \
     -H "X-Scope-OrgID: my-tenant" \
     -d "query={app=\"foo\"}" \
     -d "start=1704067200" \
     -d "end=1704153600"
   
   # RFC3339
   curl -X POST http://compactor:3100/loki/api/v1/delete \
     -H "X-Scope-OrgID: my-tenant" \
     -d "query={app=\"foo\"}" \
     -d "start=2024-01-01T00:00:00Z" \
     -d "end=2024-01-02T00:00:00Z"
   ```

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Delete request already processed

**Error message:**

```text
deletion of request which is in process or already processed is not allowed
```

**Cause:**

An attempt was made to cancel a delete request that is already being processed or has completed processing.

**Resolution:**

1. **Check the status** of the delete request:

   ```bash
   curl -s http://compactor:3100/loki/api/v1/delete \
     -H "X-Scope-OrgID: my-tenant" | jq
   ```

1. **Submit a new delete request** if you need to delete additional data.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Delete request cancellation past deadline

**Error message:**

```text
Cancellation of partially completed delete request or delete request past the deadline of <duration> since its creation is not allowed. To force, use the ?force query parameter
```

**Cause:**

A cancel request was made for a delete request that is either partially completed or past the cancellation deadline.

**Resolution:**

1. **Force the cancellation** using the `force` query parameter:
<!-- Is this even possible or did the AI hallucinate this? -->
   ```bash
   curl -X DELETE "http://compactor:3100/loki/api/v1/delete?request_id=<id>&force=true" \
     -H "X-Scope-OrgID: my-tenant"
   ```

1. **Note:** Forcing cancellation of a partially completed request means some data may already have been deleted.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: Yes (with force parameter)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Invalid max_interval for delete request

**Error message:**

```text
invalid max_interval: valid time units are 's', 'm', 'h'
```

Or:

```text
max_interval can't be greater than <configured-limit>
```

Or:

```text
max_interval can't be greater than the interval to be deleted (<duration>)
```

**Cause:**

The `max_interval` parameter on a delete request has an invalid value, exceeds the configured `delete_max_interval` limit, or exceeds the time range of the delete request itself.

**Resolution:**

1. **Use a valid time format** with supported units (`s`, `m`, `h`):

   ```bash
   curl -X POST http://compactor:3100/loki/api/v1/delete \
     -H "X-Scope-OrgID: my-tenant" \
     -d "query={app=\"foo\"}" \
     -d "start=1704067200" \
     -d "end=1704153600" \
     -d "max_interval=1h"
   ```

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No 
>>>>>>> a545d7a3c4 (docs: Troubleshooting Compactor)

## Ruler errors

<!-- Additional content in next PRs.  Just leaving the headings here for context and so that I can keep things in order if PRs merge out of sequence. -->

## Kafka integration errors



## Bloom gateway errors



## Write-ahead log (WAL) errors



## Ingester lifecycle errors



## Pattern ingester errors



## API parameter errors

