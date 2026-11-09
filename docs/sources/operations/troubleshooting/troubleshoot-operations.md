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

- **Review all listed errors** carefully - each error message describes a specific configuration problem.
- **Check your configuration file** for syntax errors and invalid values.
- **Validate your configuration** before applying:

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

- **Use only one storage backend** in your common config:

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

- **For multiple storage backends**, configure them explicitly in specific sections rather than common config.

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

- **Set the path prefix**:

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

- **Or disable persist_tokens** if you don't need token persistence:

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

- **Remove the deprecated config** and use specific gRPC client configs:

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

### Error: Invalid schema version

**Error message:**

```text
CONFIG ERROR: invalid schema version at schema position <position>
```

**Cause:**

A `schema_config` entry has a `schema` value that isn't a recognized schema version string (for example, a typo like `v31` instead of `v13`). This check runs before the other schema-related checks, so a config with this error won't also show the structured-metadata or TSDB-related errors below for the same period.

**Resolution:**

- **Check the `schema` field** in each `schema_config.configs` entry for typos:

  ```yaml
  schema_config:
    configs:
      - from: "2024-04-01"
        store: tsdb
        object_store: s3
        schema: v13  # Must be a recognized schema version
        index:
          prefix: index_
          period: 24h
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
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

- **Disable structured metadata temporarily**:

  ```yaml
  limits_config:
    allow_structured_metadata: false
  ```

- **Update your schema config** to v13 or newer:

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

- **Re-enable structured metadata** after the schema migration is complete.

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

- **Disable structured metadata temporarily** and migrate to the TSDB index type:

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

- **Re-enable structured metadata** after migrating to TSDB.

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

- **Set the common path prefix** (simplest approach):

  ```yaml
  common:
    path_prefix: /var/loki
  ```

- **Or configure directories explicitly**:

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

- **Set the common path prefix**:

  ```yaml
  common:
    path_prefix: /var/loki
  ```

- **Or set the compactor working directory explicitly**:

  ```yaml
  compactor:
    working_directory: /var/loki/compactor
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Unrecognized object store type

**Error message:**

```text
unrecognized `object_store` type `<type>`, which also does not match any named_stores. Choose one of: <supported_types>. Or choose a named_store
```

**Cause:**

The schema configuration references an object store type that Loki does not recognize.

{{< admonition type="note" >}}
TSDB is the only supported index type (the `store` field in `schema_config`), so there is no separate "unrecognized index type" validation error. If you set `store` to anything other than `tsdb`, you'll hit the "TSDB index type required for structured metadata" error (if structured metadata is enabled) or a startup failure elsewhere when Loki tries to construct the index client for that type.
{{< /admonition >}}

**Resolution:**

- **Use a supported object store type**: `s3`, `gcs`, `azure`, `swift`, `filesystem`, `bos`, `alibabacloud`

- **Or reference a valid named store** defined in your configuration:

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

### Error: Unsupported S3 storage class

**Error message:**

```text
unsupported S3 storage class (supported values: <supported_types>)
```

**Cause:**

The `storage_config.aws.storage_class` field (or the equivalent field on a named S3 store) is set to a value that isn't one of the storage classes supported by Loki's S3 client.

**Resolution:**

- **Use a supported storage class**:`STANDARD` or `STANDARD_IA`.

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

{{< admonition type="note" >}}
This error can also appear even when the overrides-exporter target isn't enabled, if the overrides subsystem fails to initialize for another reason. The underlying check treats "overrides are unavailable" as the same condition as "overrides-exporter enabled with no runtime config," so the message shown doesn't always mean the overrides-exporter target specifically is the cause.
{{< /admonition >}}

**Resolution:**

- **Configure a runtime configuration file**:

  ```yaml
  runtime_config:
    file: /etc/loki/runtime-config.yaml
  ```

- **Or disable the overrides-exporter** if not needed by removing it from your target list.

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

- **Review the runtime config** file for the specified tenant.
- **Validate the override values** against the limits configuration schema.
- **Fix invalid values** such as negative durations, invalid label matchers, or out-of-range settings. Common `<details>` values wrapped by this error include:
  - `invalid labels matchers: <details>` — a `retention_stream` selector or similar label-matcher override doesn't parse as valid LogQL label syntax.
  - `dataobj_shard_factor: shard bucket count <value> is not a power of two` — the experimental `dataobj_shard_factor` override must be a power of two.
  - `dataobj_shard_factor: shard factor <value> is more than the maximum <max>` — the override exceeds the configured maximum shard factor.

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

- **Set retention periods to at least 24 hours**:

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

- **Remove the setting** if using object storage:

  ```yaml
  # Remove or set to 0:
  query_store_max_look_back_period: 0
  ```

- **Or use filesystem storage** if this setting is needed for local development.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Query store max look back period too small

**Error message:**

```text
the configured query_store_max_look_back_period of '<value>' is less than the calculated default of '<default>', setting to default
```

**Cause:**

This is a sibling check to "Invalid query store max look back period" above: `query_store_max_look_back_period` is set to a positive value, but it's smaller than the value Loki calculates as the minimum safe default for your configuration. Unlike the error above, this message is a warning-style log line, not a fatal startup error — Loki overrides the value and continues starting up.

**Resolution:**

- **Remove the setting** to let Loki use its calculated default, or increase it to at least the logged default value:

  ```yaml
  query_store_max_look_back_period: 0  # Let Loki calculate the default
  ```

**Properties:**

- Enforced by: Module initialization
- Retryable: No (configuration should be adjusted, though Loki continues running with the corrected value)
- HTTP status: N/A (startup-time log message, not a request error)
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

- **Add the `X-Scope-OrgID` header** to your requests:

  ```bash
  curl -H "X-Scope-OrgID: my-tenant" http://loki:3100/loki/api/v1/push ...
  ```

- **For Grafana**, configure the tenant ID in the Loki data source settings under "HTTP Headers".

- **For Alloy**, set the tenant ID in the `loki.write` component:

  ```alloy
  loki.write "default" {
    endpoint {
      url       = "http://loki:3100/loki/api/v1/push"
      tenant_id = "my-tenant"
    }
  }
  ```

- **Disable multi-tenancy** for single-tenant deployments:

  ```yaml
  auth_enabled: false
  ```

**Properties:**

- Enforced by: Authentication middleware
- Retryable: Yes (with tenant ID)
- HTTP status: 401 Unauthorized (some internal or proxy-layer forwarding paths can map the same underlying error to 400 Bad Request instead)
- Configurable per tenant: No

### Error: Multiple org IDs present

**Error message:**

```text
multiple org IDs present
```

**Cause:**

The request's `X-Scope-OrgID` header contains a pipe-separated list of tenant IDs (for example `tenant1|tenant2`), but it reached an endpoint that only accepts a single tenant, such as `/loki/api/v1/push` or `/loki/api/v1/tail`. Only query endpoints support multiple tenants in this header. The ruler API wraps this same condition in its own message, `too many org ids found`, rather than returning this raw text.

**Resolution:**

- **Send a single tenant ID** in the `X-Scope-OrgID` header for write and tail requests.
- **Reserve pipe-separated tenant IDs** for query endpoints that explicitly support multi-tenant queries (`querier.multi_tenant_queries_enabled: true`).
- **For cross-tenant queries**, use pipe-separated tenant IDs only where supported:

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

- **Use a shorter tenant ID** (maximum 150 characters).

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

- **Choose a different tenant ID** that is not `.` or `..`.

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

The tenant ID contains characters that are not allowed.

**Resolution:**

- **Use only supported characters** in your tenant ID: letters (`a-z`, `A-Z`), digits (`0-9`), and the special characters `!`, `-`, `_`, `.`, `*`, `'`, `(`, `)`. For the full, authoritative list of restrictions (including the `.`/`..` rule above and the warning about colons), refer to [Restrictions](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/#restrictions) in Manage tenant isolation.

**Properties:**

- Enforced by: Tenant validation
- Retryable: Yes (with valid tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Org or user ID conflict

**Error message:**

```text
different org ID already present
```

Or:

```text
no user id
```

Or:

```text
different user ID already present
```

**Cause:**

These are internal sentinel errors for conflicting tenant (org) or user ID values, siblings of "No org ID" and "Multiple org IDs present" above. They typically surface when something internal to Loki (such as gRPC metadata propagation) tries to inject a tenant or user ID into a context that already carries a different one, or expects a user ID that was never set.

**Resolution:**

- **Check for conflicting tenant ID injection** across proxies, sidecars, or internal gRPC calls.
- **Review logs** for the specific internal call path that raised the error — these aren't typically caused by a malformed client request.

**Properties:**

- Enforced by: Tenant/user ID context propagation (dskit)
- Retryable: Depends on the underlying cause
- HTTP status: Varies (not consistently assigned)
- Configurable per tenant: No

### Error: Unsupported character in tenant metadata

**Error message:**

```text
metadata '<value>' contains unsupported character '<char>'
```

**Cause:**

Tenant IDs can optionally carry metadata as a suffix in the form `tenantID:key=value`. This metadata suffix has its own character validation, separate from tenant ID validation, and a maximum length of 64 characters.

**Resolution:**

- **Use only supported characters** in the metadata portion of the tenant ID, following the same rules as tenant IDs themselves.
- **Keep the metadata suffix under 64 characters.**

**Properties:**

- Enforced by: Tenant validation
- Retryable: Yes (with valid metadata)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Deletion not available for tenant

**Error message:**

```text
deletion is not available for this tenant
```

**Cause:**

This exact message, with an HTTP 403 response, is produced when the tenant's `deletion_mode` override resolves to `disabled` (or doesn't resolve to a delete-enabled mode). It's a per-tenant check that doesn't look at `retention_enabled` directly.

If `retention_enabled` is `false` in the compactor configuration (its default), the deletion API routes and gRPC handler are never registered in the first place, so a request to the deletion endpoints behaves differently — most commonly a 404 (route not found) — rather than this specific 403 message. If you expect the deletion API to be reachable at all, check `retention_enabled` first; if it's reachable but returns this specific message, check the tenant's `deletion_mode`.

**Resolution:**

- **Enable retention on the compactor**, which registers the deletion API endpoints in the first place:

  ```yaml
  compactor:
    retention_enabled: true
  ```

  If retention is disabled, Loki also logs `init compactor: delete store client not initialised when retention is enabled` when something tries to use the deletion client without it having been constructed — this is a related, startup-time symptom of the same root cause.

- **If needed, override the deletion mode for the tenant** in the runtime configuration:

  ```yaml
  overrides:
    my-tenant:
      deletion_mode: filter-and-delete  # Or "filter-only"
  ```

  Valid deletion modes:
  - `disabled` - Deletion is not allowed
  - `filter-only` - Lines matching delete requests are filtered at query time but not physically deleted
  - `filter-and-delete` - Lines are filtered at query time and physically deleted during compaction (default)

- **Ensure the compactor is configured** for retention:

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

- **Use a valid storage backend type**:

  - `s3` - Amazon S3 or S3-compatible storage
  - `gcs` - Google Cloud Storage
  - `azure` - Azure Blob Storage
  - `swift` - OpenStack Swift
  - `filesystem` - Local filesystem
  - `bos` - Baidu Object Storage
  - `alibabacloud` - Alibaba Cloud Object Storage Service (OSS)

  ```yaml
  schema_config:
    configs:
      - from: 2024-01-01
        store: tsdb
        object_store: s3  # Must be one of the valid types
        schema: v13
        index:
          prefix: index_
          period: 24h
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

- **Use valid characters** in your storage prefix:

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

- **Use a supported SSE type**:

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

- **Provide valid JSON** for the encryption context:

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

- **Remove the bucket name** from the endpoint and configure it separately:

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

- **Provide a valid URL** for the STS endpoint:

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

{{< admonition type="note" >}}
This error message originates in the vendored Azure SDK for Go, not in Loki's own code. The exact wording could change if Loki upgrades its Azure SDK dependency.
{{< /admonition >}}

**Resolution:**

- **Use a valid connection string format**:

  ```yaml
  storage_config:
    azure:
      # Use account credentials:
      account_name: myaccount
      account_key: mykey
      
      # Or connection string:
      connection_string: "DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=mykey;EndpointSuffix=core.windows.net"
  ```

- **Verify the connection string** in Azure Portal under Storage Account > Access Keys.

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

Or, with slightly different capitalization and wording, from the storage client factory for the remaining backends:

```text
Unrecognized named bos storage config <name>
Unrecognized named cos storage config <name>
Unrecognized named alibabacloud oss storage config <name>
```

Or for an unrecognized store type:

```text
unrecognized named storage type: <storeType>
```

**Cause:**

A named storage configuration referenced in the schema config doesn't exist in the named stores configuration.

**Resolution:**

- **Define the named store** in your configuration:

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

- **Check spelling** of the store name in both the definition and reference.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Named store conflicts with a predefined type or is already defined

**Error message:**

```text
named store "<name>" should not match with the name of a predefined storage type
```

Or:

```text
named store "<name>" is already defined under <storeType>
```

**Cause:**

A named store's name collides with a built-in storage type name (such as `s3` or `gcs`), or the same named store name is defined more than once across different storage backend types.

**Resolution:**

- **Rename the named store** to avoid conflicting with built-in type names (`s3`, `gcs`, `azure`, `swift`, `filesystem`, `bos`, `cos`, `alibabacloud`).
- **Remove the duplicate definition** if the same named store name is accidentally defined under more than one backend type in `storage_config.named_stores`.

**Properties:**

- Enforced by: Configuration validation (named store registration)
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

The `<details>` portion often includes one of these wrapped errors from Redis endpoint resolution:

- `failed to derive endpoints: <details>` — Loki couldn't resolve the configured Redis endpoint(s) into a usable address list.
- `splitting host:port failed: <details>` — one of the configured Redis addresses isn't in valid `host:port` form.
- `could not lookup host: <details>` — see the dedicated entry below.

**Resolution:**

- **Verify Redis connectivity** from the Loki host:

  ```bash
  redis-cli -h <REDIS-HOST> -p <REDIS-PORT> ping
  ```

- **Check the Redis endpoint configuration**:

  ```yaml
  chunk_store_config:
    chunk_cache_config:
      redis:
        endpoint: redis:6379
        timeout: 500ms
  ```

- **Configure authentication** if required:

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
could not lookup host: <underlying DNS error>
```

**Cause:**

DNS resolution failed for the Redis hostname. The error wraps the underlying DNS lookup failure rather than repeating just the hostname, so the actual message looks like `could not lookup host: lookup redis-bad: no such host`.

**Resolution:**

- **Verify DNS resolution**:

  ```bash
  nslookup redis-host
  ```

- **Use an IP address** if DNS is not available:

  ```yaml
  chunk_store_config:
    chunk_cache_config:
      redis:
        endpoint: 10.0.0.100:6379
  ```

- **Check your DNS configuration** and network settings.

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

- **Verify the endpoint** is actually a Redis server.
- **Check Redis health**:

  ```bash
  redis-cli -h <HOST> -p <PORT> INFO
  ```

- **Review proxy configurations** if using a load balancer in front of Redis.

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

- **Choose one cache backend** per cache type:

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

A results cache is required for the query frontend but no cache configuration was provided. A sibling validation in the same results-cache config, `unsupported compression type: <type>`, fires instead if a cache is configured but its `compression` setting isn't a recognized value.

**Resolution:**

- **Configure a cache backend**:

  ```yaml
  query_range:
    results_cache:
      cache:
        memcached:
          expiration: 1h
        memcached_client:
          addresses: memcached:11211
  ```

- **Or disable results caching** if not needed:

  ```yaml
  query_range:
    cache_results: false
  ```

**Properties:**

- Enforced by: Query frontend initialization
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Error loading cache generation numbers

**Error message:**

```text
msg="error loading cache generation numbers" err="unexpected status code: 403"
```

Or from the compactor HTTP client:

```text
msg="error getting cache gen numbers from the store" err="unexpected status code: 403"
```

**Metric:** `loki_delete_cache_gen_load_failures_total`

**Cause:**

Loki uses cache generation numbers to invalidate query caches when log deletion requests are processed. This documented message and metric come specifically from the **on-demand** lookup path: whenever a query needs the generation number for a tenant that isn't cached yet, the loader fetches it from the compactor right then. When the compactor returns HTTP 403, it means the deletion API is not enabled for the tenant. This path logs the message above and increments the `loki_delete_cache_gen_load_failures_total` metric.

{{< admonition type="note" >}}
There's a second, separate refresh path: a background ticker periodically re-fetches generation numbers for tenants that are *already* cached, independent of query traffic. If that periodic refresh fails, it logs a different message, `error reloading generation numbers`, and does **not** increment `loki_delete_cache_gen_load_failures_total`. If you see reload-related log noise without a matching metric increase, you're looking at this periodic path rather than the on-demand one described here.
{{< /admonition >}}

Other non-403 causes include:

- The compactor is unreachable (network or DNS issues)
- The compactor is not running or not ready
- TLS or authentication misconfiguration between the querier and compactor

**Resolution:**

- **Check if deletion is intentionally disabled.** If you don't use log deletion for this tenant, these errors are harmless but noisy. You can verify by sending a GET request to the compactor's cache generation number endpoint:

   ```bash
   curl -s -H "X-Scope-OrgID: <tenant>" http://compactor:3100/loki/api/v1/cache/generation_numbers
   ```

   If the response is `"deletion is not available for this tenant"`, the deletion API is not enabled for the tenant.

- **Enable deletion for the tenant** if log deletion is required:

   ```yaml
   overrides:
     <tenant>:
       deletion_mode: filter-and-delete
   ```

- **Check compactor connectivity** if the error includes a non-403 status code or a connection error:

   ```bash
   curl -s http://compactor:3100/ready
   ```

- **Verify the compactor address** is correctly configured. Queriers and other components that use the cache generation loader need to reach the compactor:

   ```yaml
   compactor:
     compactor_address: http://compactor:3100
   ```

**Properties:**

- Enforced by: Cache generation number loader (`GenNumberLoader`)
- Retryable: Yes (the documented on-demand path retries on the next query for that tenant; a separate background ticker refreshes already-cached tenants every 5 minutes, independent of this specific message/metric)
- HTTP status: N/A (background process, not a request-time error)
- Configurable per tenant: Yes (via `deletion_mode` in tenant overrides)

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

- **Check the health of ring members**:

  ```bash
  curl -s http://loki:3100/ring | jq '.shards[] | select(.state != "ACTIVE")'
  ```

- **Restart unhealthy instances** that are stuck in a bad state.
- **Scale up instances** if there aren't enough healthy members.
- **Check resource constraints** (CPU, memory, disk) on unhealthy instances.

**Properties:**

- Enforced by: Ring replication
- Retryable: Yes (after instances recover)
- HTTP status: 503 Service Unavailable (may vary by component — the specific status depends on which caller surfaces the ring error over HTTP)
- Configurable per tenant: No

### Error: Empty ring

**Error message:**

```text
empty ring
```

**Cause:**

No instances are registered in the ring. This typically occurs during initial cluster startup, for example if your ingesters are OOM crashing, or due to misconfiguration.

**Resolution:**

- **Wait for instances to register** during initial startup.
- **Check ingesters** to make sure they are running.
- **Check that all instances can communicate** over the configured ports.
- **Verify ring configuration** across all components, especially memberlist configuration:

  ```yaml
  ingester:
    lifecycler:
      ring:
        kvstore:
          store: memberlist
        replication_factor: 3
  ```

- **Check KV store health** (Consul, etcd, or memberlist):

  ```bash
  # For memberlist
  curl -s http://loki:3100/memberlist
  ```

**Properties:**

- Enforced by: Ring operations
- Retryable: Yes (after instances register)
- HTTP status: 503 Service Unavailable (may vary by component)
- Configurable per tenant: No

### Error: Instance not found in the ring

**Error message:**

```text
instance <id> not found in the ring
```

**Cause:**

A specific instance is expected to be in the ring but isn't registered. This can happen after a restart if the instance hasn't re-joined the ring yet.

**Resolution:**

- **Wait for the instance to re-register** in the ring.
- **Check the instance's logs** for ring join failures.
- **Verify KV store connectivity** from the instance.

**Properties:**

- Enforced by: Ring operations
- Retryable: Yes (after instance registers)
- HTTP status: 503 Service Unavailable (may vary by component)
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

- **Wait for token assignment** during startup.
- **Check the ring status** for the instance:
  Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

  OR

  ```bash
  curl -s http://loki:3100/ring
  ```

- **Restart the instance** if tokens are not assigned after startup completes.
- **Check KV store connectivity** and health.

**Properties:**

- Enforced by: Lifecycler readiness check
- Retryable: Yes (after tokens are assigned)
- HTTP status: 503 Service Unavailable (may vary by component)
- Configurable per tenant: No

### Error: Error talking to the KV store

**Error message:**

```text
error talking to the KV store
```

**Cause:**

The instance cannot communicate with the key-value store used for ring state. The KV store (Consul, etcd, or memberlist) is required for ring coordination.

**Resolution:**

- **Check KV store health and connectivity**:

  ```bash
  # For Consul
  curl http://consul:8500/v1/status/leader
  
  # For etcd
  etcdctl endpoint health
  ```

- **Verify network connectivity** between Loki instances and the KV store.
- **Check firewall rules** allow traffic on KV store ports.
- **For memberlist**, verify that gossip ports are accessible between all instances:

  ```yaml
  memberlist:
    bind_port: 7946
    join_members:
      - loki-memberlist:7946
  ```

**Properties:**

- Enforced by: Ring lifecycler
- Retryable: Yes (after KV store recovery)
- HTTP status: 503 Service Unavailable (may vary by component)
- Configurable per tenant: No

### Error: No ring returned from the KV store

**Error message:**

```text
no ring returned from the KV store
```

**Cause:**

The KV store responded but returned an empty or invalid ring descriptor. This can happen if the KV store was recently initialized or its data was cleared.

**Resolution:**

- **Wait for ring initialization** during first startup.
- **Check if the KV store data was accidentally cleared**.
- **Restart all ring members** to re-register if the KV store was reset.

**Properties:**

- Enforced by: Ring lifecycler
- Retryable: Yes (after ring initialization)
- HTTP status: 503 Service Unavailable (may vary by component)
- Configurable per tenant: No

### Error: Instance not found in the ring (unformatted) / inconsistent ring tokens

**Error message:**

```text
instance not found in the ring
```

**Cause:**

This is a sibling sentinel error alongside the formatted "instance `<id>` not found in the ring" error above. `instance not found in the ring` is the unformatted sentinel version used internally when no specific instance ID is available.

**Resolution:**

- **Treat the same as "Instance not found in the ring"** above: wait for the instance to re-register, check logs for ring join failures, and verify KV store connectivity.
- **For token inconsistency**, check KV store health first, since this usually indicates a corrupted or partially written ring descriptor.

**Properties:**

- Enforced by: Ring operations
- Retryable: Yes
- HTTP status: 503 Service Unavailable (may vary by component)
- Configurable per tenant: No

### Error: Cannot claim tokens in an empty ring

**Error message:**

```text
cannot claim tokens in an empty ring
```

**Cause:**

An instance tried to claim ring tokens, but the ring has no members yet. This is a startup-ordering issue rather than a true "empty ring" read error.

**Resolution:**

- **Wait for the ring to initialize** before token claiming is attempted — this usually resolves on its own as the first instance finishes registering.
- **If it persists**, check KV store connectivity and health, same as "Empty ring" above.

**Properties:**

- Enforced by: Ring lifecycler
- Retryable: Yes (after ring initializes)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Partition not found in the ring

**Error message:**

```text
partition not found in the ring
```

**Cause:**

This is the partition-ring analog of "Instance not found in the ring," for components that use Kafka-based ingestion's partition ring (see [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/)) instead of, or in addition to, the classic hash ring. A specific partition is expected to exist in the partition ring but isn't registered.

**Resolution:**

- **Check partition ring health** for the ingesters that own Kafka partitions.
- **Verify the partition ring KV store** is reachable and consistent across distributors, ingesters, queriers, and rulers.

**Properties:**

- Enforced by: Partition ring lifecycler
- Retryable: Yes (after partition registers)
- HTTP status: N/A (internal/log-only in most call sites)
- Configurable per tenant: No

### Error: Ring configuration must not change without regenerating tokens

**Error message:**

```text
Num tokens must not be changed without also changing the ring token generator
```

Or:

```text
Replication factor must not be changed without also changing the ring token generator
```

**Cause:**

This shared ring-config validation check fires in any Loki component that uses a ring with persisted tokens (for example, the query scheduler ring, the compactor ring, or the index gateway ring) when `num_tokens` or `replication_factor` has changed from what the persisted token file or ring state expects, without also changing the token generator strategy.

**Resolution:**

- **Avoid changing `num_tokens` or `replication_factor`** for a ring after tokens have already been generated and persisted, unless you also intend to regenerate tokens for that ring.
- **If the change is intentional**, clear the persisted token file or ring state for the affected component so it regenerates tokens cleanly.

**Properties:**

- Enforced by: Ring configuration validation (shared across ring-using components)
- Retryable: No (configuration must be fixed, or tokens regenerated)
- HTTP status: N/A (startup failure)
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

- **Check that join members are reachable**:

  ```bash
  # Test connectivity to seed nodes
  nc -zv loki-memberlist 7946
  ```

- **Verify DNS resolution** for join addresses:

  ```bash
  nslookup loki-memberlist
  ```

- **Check memberlist configuration**:

  ```yaml
  memberlist:
    bind_port: 7946
    join_members:
      - loki-gossip-ring.loki.svc.cluster.local:7946
  ```

- **Ensure firewall rules** allow UDP and TCP traffic on the gossip port (default 7946).
- **For Kubernetes**, verify that the headless service for memberlist is configured correctly.

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

- **Check network connectivity** between cluster members.
- **Verify other cluster members are healthy**.
- **Restart the affected instance** if automatic rejoin continues to fail.
- **Review network stability** frequent re-joins indicate underlying network issues.

**Properties:**

- Enforced by: Memberlist KV client
- Retryable: Yes (automatic retries)
- HTTP status: N/A (degraded operation)
- Configurable per tenant: No

### Error: Fast-join failed to reach minimum seed nodes

**Error message:**

```text
fast-join failed to reach minimum required seed nodes: joined %d, required %d
```

Or:

```text
failed to fast-join the memberlist cluster at startup: <details>
```

**Cause:**

Memberlist's fast-join optimization, which tries to join a minimum number of seed nodes quickly at startup before falling back to the full join process, couldn't reach enough seed nodes within its startup window.

**Resolution:**

- **Check that enough seed/join members are reachable** at startup — the same checks as "Failed to join memberlist cluster" above apply here.
- **Review the configured `join_members` list** for stale or unreachable addresses.

**Properties:**

- Enforced by: Memberlist KV client
- Retryable: Yes (falls back to the regular join process; automatic retries with backoff)
- HTTP status: N/A (startup failure or degraded operation)
- Configurable per tenant: No

{{< admonition type="note" >}}
You may also see the log-only message `service not Running` from the memberlist KV client's `JoinMembers` call, logged when a join is attempted while the underlying KV service isn't in a running state (for example, during startup or shutdown ordering). It typically resolves on its own once the service reaches a running state.
{{< /admonition >}}

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

- **Wait for the instance to restart** if this is a rolling update.
- **Check if the shutdown is expected** (maintenance, scaling down).
- **Review orchestrator logs** (Kubernetes, systemd) if the shutdown is unexpected.

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

- **Check Loki logs** for errors from the listed services.
- **Verify configuration** for the affected services.
- **Check resource availability** (memory, disk, CPU).
- **Restart the instance** if services are stuck.

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

- **Wait for startup to complete** - ingesters take time to join the ring and become ready.
- **Check ring membership**:
  Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

  OR

  ```bash
  curl -s http://ingester:3100/ring
  ```

- **Review logs** for startup errors.
- **Adjust the minimum ready duration** if startup is too slow:

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

{{< admonition type="note" >}}
The same "not ready" pattern applies to a few other components, each with its own prefix:
- `Pattern Ingester not ready: <details>` — the pattern ingester (see [Pattern ingester errors](#pattern-ingester-errors)).
- `Ingest Limits not ready: <details>` — the optional ingest-limits service.
- `Ingest Limits Frontend not ready: <details>` — the ingest-limits frontend.

All three follow the same startup/ring-join reasoning as the ingester readiness check above, and return HTTP 503 Service Unavailable from `/ready`.
{{< /admonition >}}

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

- **Check that queriers are running** and healthy.
- **Verify querier configuration** points to the correct frontend address:

  ```yaml
  frontend_worker:
    frontend_address: query-frontend:9095
  ```

- **Check gRPC connectivity** between queriers and the frontend:

  ```bash
  # Test gRPC port connectivity
  nc -zv query-frontend 9095
  ```

- **Review querier logs** for connection errors.

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

- **Check that query schedulers are running** and healthy.
- **Verify scheduler address configuration**:

  ```yaml
  frontend_worker:
    scheduler_address: query-scheduler:9095
  ```

- **Check gRPC connectivity** between the frontend and schedulers.
- **Review query scheduler logs** for errors.

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

- `limits_config.max_push_size` (CLI flag `-distributor.max-push-size`): 2GB. The same value is used as the limit for both the compressed and decompressed request size, and it is configurable per tenant.

**Resolution:**

- **Increase the distributor push size limit**:

  ```yaml
  limits_config:
    max_push_size: 4GB
  ```

- **Reduce push batch sizes** in your log shipping client (Alloy, etc.) to send smaller individual requests.

- **Reduce the amount of data per request** by lowering the batch size or flush interval in your client.

**Properties:**

- Enforced by: Distributor push handler
- Retryable: No (request must be smaller or limits increased)
- HTTP status: 413 Request Entity Too Large (both the compressed and decompressed variants return 413)
- Configurable per tenant: Yes

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

- **Reduce query scope** to return fewer results:
  - Add more specific label matchers
  - Reduce the time range
  - Lower the entries limit

- **Increase gRPC message size limits** if needed. Apply these settings to querier nodes:

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

- **Verify certificate files exist** and are readable:

  ```bash
  ls -la /path/to/cert.pem /path/to/key.pem /path/to/ca.pem
  ```

- **Check file permissions** (the Loki process must be able to read them).

- **Validate the certificate format**:

  ```bash
  openssl x509 -in /path/to/cert.pem -noout -text
  openssl rsa -in /path/to/key.pem -check
  ```

- **Verify cert and key match**:

  ```bash
  openssl x509 -noout -modulus -in cert.pem | md5sum
  openssl rsa -noout -modulus -in key.pem | md5sum
  # Both should produce the same hash
  ```

- **Check your TLS configuration**:

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

### Error: Certificate given without a key, or key given without a certificate

**Error message:**

```text
certificate given but no key configured
```

Or:

```text
key given but no certificate configured
```

**Cause:**

A lower-severity sibling of "TLS certificate loading failed" above: a `cert_file`/`cert` value was provided without a matching `key_file`/`key`, or vice versa. TLS requires both a certificate and its key together.

**Resolution:**

- **Provide both `cert_file` and `key_file`** (or neither) in the same TLS config block:

  ```yaml
  server:
    http_tls_config:
      cert_file: /path/to/cert.pem
      key_file: /path/to/key.pem
  ```

**Properties:**

- Enforced by: TLS configuration validation
- Retryable: No (configuration must be fixed)
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

- **Review TLS settings** for compatibility issues.
- **Use supported TLS versions** by setting `tls_min_version` at the top level of the `server` block:

  ```yaml
  server:
    tls_min_version: VersionTLS12
  ```

  Valid values are `VersionTLS10`, `VersionTLS11`, `VersionTLS12`, and `VersionTLS13`. There is no `max_version` setting; `tls_min_version` is the only version constraint.

- **Check cipher suite configuration** if customized.

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

- **Check DNS server availability** and configuration.
- **Verify hostname resolution**:

  ```bash
  nslookup <hostname>
  dig <hostname>
  ```

- **Use IP addresses** as a workaround if DNS is unreliable:

  ```yaml
  # Instead of dns+hostname:port
  memberlist:
    join_members:
      - 10.0.0.1:7946
      - 10.0.0.2:7946
  ```

- **For Kubernetes**, ensure CoreDNS is healthy and headless services are configured correctly.

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

- **Check scheduler logs** for startup errors or crashes.
- **Verify scheduler health**:

  ```bash
  curl -s http://scheduler:3100/ready
  ```

- **Check scheduler ring membership** if using ring-based scheduling:

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

- **Scale out queriers** to process queries faster:

  ```yaml
  querier:
    max_concurrent: 10
  ```

- **Increase queue capacity** (with caution). The default is `32000`; increase beyond that only if you have confirmed the system can handle the additional load.  Note that increasing the queue is often necessary because of how many subqueries can be generated by large values for `tsdb_max_query_parallelism`. Generally it's preferable to add more queriers and leave this setting unchanged.

  ```yaml
  query_scheduler:
    max_outstanding_requests_per_tenant: 64000
  ```

- **Rate limit queries** at the client or load balancer level.
- **Optimize slow queries** to reduce queue time.

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

- **Check the relevant parallelism setting for your index type.** For TSDB indexes (the current default), `tsdb_max_query_parallelism` supersedes `max_query_parallelism`. Either value being set to zero triggers this error. Verify that both are greater than zero:

  ```yaml
  limits_config:
    max_query_parallelism: 32          # default; applies to non-TSDB schemas
    tsdb_max_query_parallelism: 128    # default; applies to TSDB schemas
  ```

- **Size `tsdb_max_query_parallelism` to your ingest volume.** Typical values in production are in the range of 128–2048, proportional to the volume of logs ingested per day:

  | Daily ingest volume | Typical value |
  |---|---|
  | Low–moderate | 128–256 |
  | High | 512 |
  | Tens of TB/day | 1024–2048 |

- **Account for the querier capacity this requires.** Each unit of parallelism consumes one querier worker slot. With the default `querier.max_concurrent` of `4`, the number of queriers needed to fully parallelize a single query is:

  ```bash
  queriers needed = tsdb_max_query_parallelism / max_concurrent
  ```

  For example, `tsdb_max_query_parallelism: 2048` with `max_concurrent: 4` requires 512 queriers to run one query fully in parallel. Production deployments supporting many tenants running large queries simultaneously commonly run thousands of queriers.

- **Contact your administrator** if you don't have access to change these settings.

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

- **Check frontend configuration** to ensure the address is set:

  ```yaml
  frontend:
    address: query-frontend:9095
  ```

- **Verify gRPC connectivity** between frontend and scheduler.

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

- **Wait for shutdown to complete** and the scheduler to restart.
- **Check if this is expected** (rolling update, maintenance).
- **Retry the request** after the scheduler is healthy.

**Properties:**

- Enforced by: Frontend scheduler worker
- Retryable: Yes (after scheduler restart)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Unknown request type or scheduler ring misconfiguration

**Error message:**

```text
unknown request type
```

Or at startup:

```text
ring manager can't be empty when use_scheduler_ring is true
```

Or:

```text
ring manager must be initialized in ServerMode for query schedulers
```

Or when the queue hierarchy is too deep:

```text
desired queue level exceeds maxium depth of queue hierarchy: <value>
```

**Cause:**

`unknown request type` indicates the scheduler received a request it doesn't recognize, which usually points to a version mismatch between the scheduler and its frontend or querier workers. The two `ring manager` errors are startup-time configuration checks that fire when `query_scheduler.use_scheduler_ring` is enabled but the scheduler's ring manager isn't available or the component isn't running in server mode. The `desired queue level exceeds maxium depth` error (the "maxium" typo exists in the source code itself) fires when a configured queue hierarchy depth is too large.

**Resolution:**

- **For "unknown request type"**: ensure schedulers, frontends, and queriers are running compatible Loki versions.
- **For the ring manager errors**: verify `use_scheduler_ring` configuration and that the scheduler target is running in the expected server mode.
- **For the queue depth error**: reduce the configured queue hierarchy depth.

**Properties:**

- Enforced by: Query scheduler
- Retryable: No (version mismatch or configuration must be fixed)
- HTTP status: N/A (startup failure or internal protocol error)
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

- **Check index gateway health**:

  ```bash
  curl -s http://index-gateway:3100/ready
  ```

- **View the ring status**:
  Open a browser and navigate to http://localhost:3100/ring. You should see the Loki Ring Status page.

  OR

  ```bash
  curl -s http://index-gateway:3100/ring
  ```

- **Check logs** for errors preventing the gateway from becoming healthy.
- **Restart the index gateway** if it's stuck in an unhealthy state.

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

- **Check if any index gateways are running**:

  ```bash
  curl -s http://index-gateway:3100/ring | jq '.shards | length'
  ```

- **Verify ring mode is configured** if using shuffle sharding. The index gateway must run in `ring` mode and the per-tenant shard size must be set:

  ```yaml
  index_gateway:
    mode: ring

  limits_config:
    index_gateway_shard_size: 3  # default = 0 (use all instances)
  ```

- **Scale up index gateways** if needed.

**Properties:**

- Enforced by: Index gateway client
- Retryable: Yes
- HTTP status: 500 Internal Server Error
- Configurable per tenant: Yes (via `index_gateway_shard_size` in `limits_config`)

### Error: Index gateway mode not supported

**Error message:**

```text
mode %s not supported. list of supported modes: simple (default), ring
```

**Cause:**

The `index_gateway.mode` configuration value isn't one of the supported modes.

**Resolution:**

- **Use a supported mode**:

  ```yaml
  index_gateway:
    mode: simple  # Or "ring"
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Index gateway concurrency and client limits

**Error message:**

```text
index gateway max concurrent must be greater than or equal to 0
```

Or:

```text
index gateway max concurrent queue timeout must be greater than or equal to 0
```

Or:

```text
index gateway client max-in-flight-requests must be greater than or equal to 0
```

Or:

```text
index gateway client max-retries must be greater than or equal to -1
```

**Cause:**

One of the index gateway's concurrency or client-retry settings is configured with an invalid (negative, below the allowed minimum) value.

**Resolution:**

- **Use non-negative values** for `index_gateway.max_concurrent`, `index_gateway.max_concurrent_queue_timeout`, and the index gateway client's `max_in_flight_requests` — and a value of `-1` or higher for `max_retries`.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

{{< admonition type="note" >}}
When the index gateway is overloaded, requests can also fail with a generic request-shedding error from Loki's shared concurrency gate (`gate.ErrMaxConcurrent`). This is a user-facing error returned to the querier client rather than a configuration error, and is resolved the same way as other overload conditions: scale up index gateways or reduce query concurrency.
{{< /admonition >}}

## Compactor and retention errors

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

- **Verify the table should be empty**:

  ```bash
  # Check if data exists for the time period
  logcli query '{job=~".+"}' --from="<table-start-time>" --to="<table-end-time>" --limit=1
  ```

- **If the table is legitimately empty**, manually delete it from object storage.
- **If data should exist**, investigate potential data loss.

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

- **Configure the delete request store**:

  ```yaml
  compactor:
    retention_enabled: true
    delete_request_store: s3
  ```

- **Or disable retention** if not needed:

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

- **Set a valid parallelism value**:

  ```yaml
  compactor:
    max_compaction_parallelism: 1  # Must be >= 1
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Deletion marker or manifest store prefix must end with a slash

**Error message:**

```text
deletion marker object store prefix must end with /
```

Or:

```text
deletion manifest store prefix must end with /
```

**Cause:**

The compactor's configured prefix for deletion marker or deletion manifest objects in the store doesn't end with a trailing `/`.

**Resolution:**

- **Add a trailing slash** to the configured prefix. The two config fields that trigger these errors are `deletion_marker_object_store_prefix` and `deletion_manifest_store_prefix`:

  ```yaml
  compactor:
    deletion_marker_object_store_prefix: markers/
    jobs_config:
      deletion_manifest_store_prefix: manifests/
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

- **List existing delete requests**:

  ```bash
  curl -s http://compactor:3100/loki/api/v1/delete | jq
  ```

- **Verify the delete request ID** is correct.
- **Check if the request has already been processed** and removed.

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

- **Enable retention** in the compactor:

  ```yaml
  compactor:
    retention_enabled: true
    delete_request_store: s3
  ```

- **Restart the compactor** after changing the configuration.

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

- **Use Unix seconds or RFC3339 format**:

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

- **Check the status** of the delete request:

  ```bash
  curl -s http://compactor:3100/loki/api/v1/delete \
    -H "X-Scope-OrgID: my-tenant" | jq
  ```

- **Submit a new delete request** if you need to delete additional data.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No
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

- **Use a valid time format** with supported units (`s`, `m`, `h`):

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

### Error: Other invalid delete request parameters

**Error messages:**

```text
query not set
start time not set
deletes in the future are not allowed
start time can't be greater than or equal to end time
not unix seconds
```

**Cause:**

These are additional parameter-validation errors for delete requests, siblings of the time-format and `max_interval` errors above: a required field is missing (`query`, `start`), the request specifies a future deletion time, `start` isn't before `end`, or a numeric time value isn't valid Unix seconds.

**Resolution:**

- **Review the delete request parameters** and ensure `query` and `start` are set, `start` is before `end`, the range isn't in the future, and any Unix-second values are valid integers.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Delete request cancellation deadline exceeded

**Error message:**

```text
Cancellation of partially completed delete request or delete request past the deadline of <duration> is not allowed
```

**Cause:**

An attempt was made to cancel a delete request that has either started partial processing or has existed longer than the configured cancellation deadline.

**Resolution:**

- **Submit a new delete request** instead of canceling one that's already past the cancellation window.
- **Check the delete request's age and status** before attempting to cancel it.

**Properties:**

- Enforced by: Compactor delete request handler
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: No

{{< admonition type="note" >}}
You may also see the log-only messages `error getting cache generation number` and `error updating cache generation number` from the same delete-request handler. These relate to the cache generation number mechanism described in [Error loading cache generation numbers](#error-error-loading-cache-generation-numbers) earlier in this guide.
{{< /admonition >}}

## Ruler errors

Ruler errors occur when evaluating alerting rules or recording rules.

### Error: Invalid ruler evaluation config

**Error message:**

```text
invalid ruler evaluation config: <details>
```

**Cause:**

The ruler evaluation mode configuration is invalid.

**Resolution:**

- **Use a valid evaluation mode**:

  ```yaml
  ruler:
    evaluation:
      mode: local  # Or "remote"
  ```

**Properties:**

- Enforced by: Ruler module initialization (`initRuleEvaluator`)
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Remote write enabled but no clients configured

**Error message:**

```text
remote-write enabled but no clients are configured
```

Or when multiple clients are configured in the `clients` map and one entry is missing a URL:

```text
remote-write enabled but client '<name>' URL for tenant <client-id> is not configured
```

**Cause:**

Remote write is enabled for the ruler but no clients are configured. The first variant occurs when the `clients` map is empty. The second occurs when a named entry in the `clients` map has no `url` set; `<client-id>` is the map key for that entry, not a tenant ID.

**Resolution:**

- **Configure the remote write URL**:

  ```yaml
  ruler:
    remote_write:
      enabled: true
      clients:
        primary:
          url: http://prometheus:9090/api/v1/write
  ```

- **Or disable remote write**:

  ```yaml
  ruler:
    remote_write:
      enabled: false
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

{{< admonition type="note" >}}
A related per-tenant validation error, `invalid remote write client for tenant "<tenant>": <details>`, fires at runtime when the ruler tries to construct a remote-write client for a specific tenant and that tenant's client configuration fails validation. Fix it by correcting the tenant-level remote-write override in the runtime configuration.
{{< /admonition >}}

### Error: Rule result is not a vector or scalar

**Error message:**

```text
rule result is not a vector or scalar
```

**Cause:**

A rule evaluation returned an unexpected result type. Both recording rules and alerting rules must produce vector or scalar results. A plain log-stream expression (one that returns log lines rather than a numeric metric) triggers this error in either rule type.

**Resolution:**

- **Check the rule expression** returns a vector or scalar:

  ```yaml
  # Valid - returns vector:
  record: my_metric
  expr: sum(rate({job="app"}[5m] | json | level="error"))

  # Invalid - returns logs (triggers error for both recording and alerting rules):
  # record: my_metric
  # expr: '{job="app"}'
  ```

- **Use aggregation functions** to produce numeric results from log queries.

**Properties:**

- Enforced by: Ruler evaluation
- Retryable: No (rule must be fixed)
- HTTP status: N/A (background process)
- Configurable per tenant: No

### Error: Remote rule evaluation errors

**Error messages:**

```text
failed to retrieve tenant ID from context
remote rule evaluation exceeded deadline of <seconds>s
unsuccessful/unexpected response - status code <code>
<bytes> bytes exceeds response size limit of <limit>
unexpected body encoding, not valid JSON
query response error: status <status>
unsupported result type: <type>
```

**Cause:**

These errors are specific to `ruler.evaluation.mode: remote` (see "Invalid ruler evaluation config" above), where the ruler sends rule queries to an external query endpoint instead of evaluating them locally. They cover tenant-context propagation, request timeouts, unexpected HTTP responses, oversized responses, malformed JSON, and unsupported result types from the remote endpoint.

**Resolution:**

- **Check connectivity and health** of the remote query endpoint configured for rule evaluation.
- **Increase the evaluation timeout** if evaluations are legitimately slow.
- **Verify the remote endpoint returns valid JSON** with a vector or scalar result, consistent with "Rule result is not a vector or scalar" above.
- **Check response size limits** if responses are being rejected for exceeding the configured limit.

**Properties:**

- Enforced by: Remote rule evaluator
- Retryable: Depends on the specific error (timeouts and connectivity issues are retryable; malformed responses typically aren't)
- HTTP status: N/A (background process)
- Configurable per tenant: No

### Error: Ruler WAL closed

**Error message:**

```text
WAL storage closed
```

**Cause:**

An operation was attempted on the ruler's write-ahead log (WAL) after it was closed. This typically occurs during shutdown.

**Resolution:**

- **Wait for the ruler to restart** if it's restarting.
- **Check ruler logs** for errors that caused unexpected WAL closure.
- **Verify disk space** is available for WAL operations.

**Properties:**

- Enforced by: Ruler WAL
- Retryable: Yes (after ruler restart)
- HTTP status: N/A
- Configurable per tenant: No

{{< admonition type="note" >}}
Other WAL errors you may see in ruler logs include `already closed` (an operation on an already-closed WAL segment) and `unknown series ref. when trying to add exemplar: <ref>` (an internal consistency check in the WAL's exemplar handling). Both are siblings of "WAL storage closed" above and resolved the same way.
{{< /admonition >}}

{{< admonition type="note" >}}
The ruler's rules API also validates the `type` query parameter on endpoints like `/prometheus/api/v1/rules`, returning `not supported value "<value>"` (HTTP 400) if it's set to anything other than `alert` or `record`. This is unrelated to tenant or org ID handling despite sharing a response helper with some multi-tenant error cases.
{{< /admonition >}}

{{< admonition type="note" >}}
You may also see the generic log message `rule evaluation failed: <details>` wrapping any of the rule evaluation errors in this section. It is a log-only wrapper that doesn't add new information beyond the underlying error in `<details>`.
{{< /admonition >}}

## Kafka integration errors

These errors occur when Loki is configured to use Kafka for ingestion.

### Error: Missing Kafka address

**Error message:**

```text
the Kafka address has not been configured
```

**Cause:**

Kafka ingestion is enabled but no Kafka broker address is configured.

**Resolution:**

- **Configure the Kafka address**:

  ```yaml
  kafka_config:
    topic: loki-logs
    reader_config:
      address: kafka:9092
    writer_config:
      address: kafka:9092
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Missing Kafka topic

**Error message:**

```text
the Kafka topic has not been configured
```

**Cause:**

Kafka ingestion is enabled but no topic name is configured.

**Resolution:**

- **Configure the Kafka topic**:

  ```yaml
  kafka_config:
    topic: loki-logs
    reader_config:
      address: kafka:9092
    writer_config:
      address: kafka:9092
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Inconsistent SASL username and password

**Error message:**

```text
both sasl username and password must be set
```

**Cause:**

Only one of the Simple Authentication and Security Layer (SASL) username or password is configured. Both must be set together.

**Resolution:**

- **Configure both username and password**:

  ```yaml
  kafka_config:
    sasl_username: my-user
    sasl_password: ${KAFKA_PASSWORD}
  ```

- **Or remove both** if SASL authentication is not required.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Kafka enabled in distributor but not in ingester

**Error message:**

```text
kafka is enabled in distributor but not in ingester
```

**Cause:**

Kafka is configured for the distributor but the ingester isn't configured to read from Kafka. Both must be configured together.

**Resolution:**

- **Enable Kafka in both distributor and ingester**:

  ```yaml
  distributor:
    kafka_writes_enabled: true

  ingester:
    kafka_ingestion:
      enabled: true
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid producer max record size bytes

**Error message:**

```text
the configured producer max record size bytes must be a value between 1048576 and 15983616
```

**Cause:**

The `kafka_config.producer_max_record_size_bytes` value is outside the allowed range.

**Resolution:**

- **Set `producer_max_record_size_bytes` within the allowed range**:

  ```yaml
  kafka_config:
    producer_max_record_size_bytes: 15983616
  ```

- We strongly recommend leaving this value at its default unless you're testing in a development environment.

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid producer max inflight requests per broker

**Error message:**

```text
the configured producer max inflight requests per broker must be greater than or equal to 1
```

**Cause:**

The `kafka_config.producer_max_inflight_requests_per_broker` value is less than `1`.

**Resolution:**

- **Set `producer_max_inflight_requests_per_broker` to `1` or higher**:

  ```yaml
  kafka_config:
    producer_max_inflight_requests_per_broker: 20
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid producer linger

**Error message:**

```text
the configured producer linger must be greater than or equal to 0
```

**Cause:**

The `kafka_config.producer_linger` value is negative.

**Resolution:**

- **Set `producer_linger` to `0` or a positive duration**:

  ```yaml
  kafka_config:
    producer_linger: 50ms
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Partition ring required for Kafka writes

**Error message:**

```text
partition ring is required for kafka writes
```

**Cause:**

`distributor.kafka_writes_enabled` is `true`, but the distributor wasn't started with a partition ring. This usually means the partition ring key-value store isn't configured consistently across distributors and ingesters.

**Resolution:**

- **Configure the partition ring key-value store on both distributors and ingesters**:

  ```yaml
  ingester:
    kafka_ingestion:
      enabled: true
      partition_ring:
        kvstore:
          store: memberlist
  ```

- Ensure every component that participates in Kafka-based ingestion (distributors, ingesters, queriers, and rulers) uses the same key-value store configuration.

**Properties:**

- Enforced by: Distributor startup
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Single entry size exceeds maximum

**Error message:**

```text
single entry size (%d) exceeds maximum allowed size (%d)
```

**Cause:**

A single log line is larger than the configured `kafka_config.producer_max_record_size_bytes`. Loki splits large write requests into multiple Kafka records, but it can't split a single log line across records.

**Resolution:**

- **Reduce the size of the log line** at the source before sending it to Loki.
- **Enforce a maximum log line size** with `limits_config.max_line_size` (default: 256KB). Lines that exceed this limit are discarded before they reach the Kafka producer. Set `limits_config.max_line_size_truncate: true` to truncate oversized lines instead of discarding them:

  ```yaml
  limits_config:
    max_line_size: 256KB
    max_line_size_truncate: true
  ```

- **Increase `producer_max_record_size_bytes`**, within the allowed range, if your Kafka cluster supports larger records:

  ```yaml
  kafka_config:
    producer_max_record_size_bytes: 15983616
  ```

**Properties:**

- Enforced by: Kafka record encoding
- Retryable: No
- HTTP status: 400 (Bad Request)
- Configurable per tenant: No (`limits_config.max_line_size` and `limits_config.max_line_size_truncate` are configurable per tenant)

### Error: Dataobj builder topic required

**Error message:**

```text
topic is required
```

**Cause:**

The experimental data object builder, used for columnar storage, is enabled but no Kafka topic is configured for it.

**Resolution:**

- **Configure the topic for the data object builder**:

  ```yaml
  dataobj:
    builder:
      topic: loki-logs
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Ingest-limits configuration errors

**Error messages:**

```text
num partitions must be greater than 0
consumer group must be set
topic must be set
```

**Cause:**

The optional ingest-limits service is enabled (`ingest_limits.enabled: true`), but one or more of its required Kafka settings are missing or invalid.

**Resolution:**

- **Configure all required ingest-limits Kafka settings**:

  ```yaml
  ingest_limits:
    enabled: true
    num_partitions: 64
    consumer_group: ingest-limits
    topic: loki-ingest-limits
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

{{< admonition type="note" >}}
The ingest-limits service has additional rate-tracking configuration validation errors in the same family: `active window must be greater than 0`, `rate window must be greater than 0`, `bucket size must be greater than 0`, `rate window must be a multiple of bucket-size`, and `eviction interval must be greater than 0`. Fix these the same way: correct the corresponding duration or size setting under `ingest_limits` to a valid positive value.

The ingest-limits *frontend* (a separate component from the ingest-limits service) has its own cache-related validation error, `assigned partitions cache TTL must be a positive number`, and similar checks for its other cache TTL settings.
{{< /admonition >}}

### Error: Ingest-limits readiness errors

**Error messages:**

```text
waiting to be assigned some partitions
partitions are not ready
```

**Cause:**

The ingest-limits service hasn't finished joining the partition ring yet, or one or more of its assigned partitions aren't ready. This is usually a transient condition during startup or a rolling restart.

**Resolution:**

- **Wait for the service to finish starting.** These errors should clear on their own once the partition ring stabilizes.
- **If the errors persist**, check connectivity between the ingest-limits service and the Kafka brokers, and check that the configured partition ring key-value store is reachable.

**Properties:**

- Enforced by: Ingest-limits service readiness check
- Retryable: Yes (transient during startup)
- HTTP status: 503 (Service Unavailable)
- Configurable per tenant: No

### Error: Generic Kafka protocol errors

**Error messages:**

```text
unexpected response type
malformed response
```

**Cause:**

The Kafka client received a response from the broker that it didn't expect, or couldn't parse. This usually points to a Kafka, or Kafka-protocol-compatible broker, that uses a protocol version Loki doesn't fully support, or to a network issue that corrupted the response.

**Resolution:**

- **Check your Kafka broker version** against the version supported by Loki's Kafka client ([franz-go](https://github.com/twmb/franz-go)).
- **Check network connectivity** between Loki and the Kafka brokers for packet loss or truncation.

**Properties:**

- Enforced by: Kafka client (offset manager)
- Retryable: Yes
- HTTP status: N/A
- Configurable per tenant: No

{{< admonition type="note" >}}
A newer, parallel Kafka client implementation (`kafkav2`) has its own `unexpected response type` error. If you're using a component built on this newer client, the same resolution steps apply.
{{< /admonition >}}

### Error: Pattern ingester Kafka consumer group not set

**Error message:**

```text
pattern ingester kafka consumer group must be set
```

**Cause:**

The pattern ingester's Kafka ingestion is enabled (`pattern_ingester.kafka_ingestion.enabled: true`), but no consumer group is configured.

**Resolution:**

- **Set a consumer group** for the pattern ingester's Kafka ingestion:

  ```yaml
  pattern_ingester:
    kafka_ingestion:
      enabled: true
      consumer_group: pattern-ingester
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## Bloom gateway errors

Bloom gateway errors occur when using bloom filters for query acceleration.

{{< admonition type="warning" >}}
Query acceleration using bloom filters is deprecated and will be removed in a future release. This covers the Bloom Planner, Bloom Builder, and Bloom Gateway components and the `bloom_build`, `bloom_gateway`, and `storage_config.bloom_shipper` configuration blocks, along with the per-tenant `bloom_*` limits. The components and settings still work today, and each bloom component logs a deprecation warning on startup. For details, refer to [Upgrade](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#bloom-filters-are-deprecated).
{{< /admonition >}}

### Error: Invalid bloom gateway addresses

**Error message:**

```text
addresses requires a list of comma separated strings in DNS service discovery format with at least one item
```

**Cause:**

The `bloom_gateway.client.addresses` configuration field is empty or unset.

**Resolution:**

- **Configure valid addresses**:

  ```yaml
  bloom_gateway:
    client:
      addresses: dns+bloom-gateway:9095
  ```

  Valid formats:
  - `dns+hostname:port` - DNS-based discovery
  - `host1:port,host2:port` - Static list

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Request time range must span exactly one day

**Error message:**

```text
request time range must span exactly one day
```

**Cause:**

Bloom gateway requests must be for exactly one day of data due to how bloom blocks are organized.

**Resolution:**

- This is typically handled automatically by the bloom querier, which splits multi-day queries into per-day requests before sending them to the gateway. If you see this error:
  - **Check that the querier is properly configured**
  - **Ensure queries are routed through the querier**

**Properties:**

- Enforced by: Bloom gateway
- Retryable: No
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: From time must not be after through time

**Error message:**

```text
from time must not be after through time
```

**Cause:**

The bloom gateway received a request where the start time (`from`) is later than the end time (`through`).

**Resolution:**

- This indicates a malformed request reaching the bloom gateway. Verify that the client sending the request constructs time ranges correctly with `from` before or equal to `through`.

**Properties:**

- Enforced by: Bloom gateway
- Retryable: No
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: Could not parse block key

**Error message:**

```text
could not parse block key
```

**Cause:**

The bloom gateway couldn't parse the object storage key for a bloom block, usually because of an unexpected key format from a corrupted or unexpectedly structured bloom block path.

**Resolution:**

- **Check bloom block storage** for unexpected files or naming under the bloom shipper's storage prefix.
- **Review bloom builder and planner logs** for errors that might have produced a malformed block key.

**Properties:**

- Enforced by: Bloom gateway
- Retryable: No
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

## Write-ahead log (WAL) errors

WAL errors occur when the ingester cannot properly manage its write-ahead log.

### Error: WAL is stopped

**Error message:**

```text
wal is stopped
```

**Cause:**

An operation was attempted on the WAL after it was stopped. This typically occurs during shutdown or after a fatal error.

**Resolution:**

- **Check ingester health and logs** for errors.
- **Verify disk space** is available.
- **Restart the ingester** if it's in a bad state.

**Properties:**

- Enforced by: Ingester WAL
- Retryable: Yes (after ingester restart)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: Invalid checkpoint duration

**Error message:**

```text
invalid checkpoint duration: <duration>
```

**Cause:**

The WAL checkpoint duration is set to an invalid value (likely zero or negative).

**Resolution:**

- **Set a valid checkpoint duration**:

  ```yaml
  ingester:
    wal:
      checkpoint_duration: 5m
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Invalid disk full threshold

{{< admonition type="note" >}}
The `disk_full_threshold` configuration option was introduced in Loki 3.7. This error does not occur in earlier releases.
{{< /admonition >}}

**Error message:**

```text
invalid disk full threshold: <value> (must be between 0 and 1)
```

**Cause:**

The WAL disk full threshold is set to a value outside the valid range. Valid values are between 0 and 1 (inclusive), where 0 disables throttling and values greater than 0 represent the fraction of disk capacity at which writes are throttled.

**Resolution:**

- **Set a valid threshold**:

  ```yaml
  ingester:
    wal:
      disk_full_threshold: 0.9  # 90%
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## Ingester lifecycle errors

Ingester lifecycle errors occur during ingester startup, shutdown, or state transitions.

### Error: Ingester is shutting down

**Error message:**

```text
Ingester is shutting down
```

**Cause:**

The ingester is in the process of shutting down and is no longer accepting writes. This error (also known as `ErrReadOnly`) is returned when a push request arrives during graceful shutdown. During this period the ingester may still serve reads for data it holds in memory.

The same `ErrReadOnly` message is also reused for a different trigger: when the ingester's write-ahead log disk usage exceeds `wal.disk_full_threshold` (see [Invalid disk full threshold](#error-invalid-disk-full-threshold)), the ingester throttles writes using this same "shutting down" message even though it isn't actually shutting down. If you see this error on an ingester that isn't restarting, check disk usage before assuming a shutdown is in progress.

**Resolution:**

- **Configure clients to retry** with backoff. The distributor will route to other healthy ingesters.
- **Wait for shutdown to complete** and the new instance to start.
- **Check if shutdown is expected** (rolling update, scaling event).
- **If unexpected**, check orchestrator logs for OOM kills or health check failures.

**Properties:**

- Enforced by: Ingester
- Retryable: Partial. The distributor sends writes to all ingesters in the replication set in parallel and uses a quorum model. If the remaining ingesters meet the minimum success threshold, the overall write succeeds despite this error from a shutting-down ingester.
- HTTP status: 500 Internal Server Error (the error is returned as a plain Go error from the push path rather than an explicitly coded gRPC status, so it defaults to an internal-error-equivalent status rather than being explicitly assigned)
- Configurable per tenant: No

### Error: Ingester is rejecting writes: WAL disk usage exceeded threshold

**Error message:**

```text
Ingester is rejecting writes: WAL disk usage exceeded threshold
```

**Cause:**

Disk usage on the volume that stores the WAL has reached the configured threshold (`-ingester.wal-disk-full-threshold`, default 90%), so the ingester rejects new writes until usage drops below it. The ingester is otherwise running and continues to serve reads. The ingester logs `disk usage exceeded threshold, throttling writes` when throttling starts.

**Resolution:**

- **Free up or increase disk space** for the WAL directory (`-ingester.wal-dir`).
- **Monitor disk usage** with the `loki_ingester_wal_disk_usage_percent` metric.
- Refer to [WAL disk full](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/troubleshooting/troubleshoot-ingest/#error-wal-disk-full) for more details.

**Properties:**

- Enforced by: Ingester
- Retryable: Partial. As with `Ingester is shutting down`, the write succeeds if the remaining ingesters in the replication set meet the quorum.
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: Ingester is stopping or already stopped

**Error message:**

```text
Ingester is stopping or already stopped.
```

**Cause:**

The ingester's shutdown management endpoint (`POST /loki/api/v1/ingester/shutdown`) was called when the ingester was not in a `Running` state. This happens when the endpoint is called a second time during an in-progress shutdown or after the ingester has already stopped. This error is returned by the shutdown endpoint, not by the log-write or query paths.

**Resolution:**

- **Do not call the shutdown endpoint again** while a shutdown is already in progress.
- **Check orchestrator** for duplicate shutdown signals or restart policies.
- **Investigate** if the stop was unexpected (pod eviction, OOM, crash).

**Properties:**

- Enforced by: Ingester shutdown endpoint
- Retryable: No (the shutdown endpoint call itself is not retryable; wait for the ingester to restart before sending new writes)
- HTTP status: 503 Service Unavailable (response from the shutdown endpoint)
- Configurable per tenant: No

### Error: Failed to start partition reader

**Error message:**

```text
failed to start partition reader: <details>
```

**Cause:**

The ingester could not start its Kafka partition reader. This occurs when Kafka ingestion is enabled but the partition reader fails to initialize.

**Resolution:**

- **Check Kafka connectivity** from the ingester.
- **Verify Kafka topic exists** and the ingester has appropriate permissions.
- **Review Kafka configuration**:

  ```yaml
  kafka:
    address: kafka:9092
    topic: loki-logs
  ```

- **Check Kafka broker health**.

**Properties:**

- Enforced by: Ingester startup
- Retryable: No (configuration or infrastructure must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Owned streams service startup errors

**Error messages:**

```text
can not start recalculate owned streams service: <details>
can not ensure recalculate owned streams service is running: <details>
failed to check ingester shutdown marker: <details>
```

**Cause:**

These are additional ingester startup errors, siblings of the partition reader and partition ring lifecycler errors above. The "owned streams" service periodically recalculates which streams an ingester owns in the ring, which affects limit enforcement; the shutdown marker check looks for a marker file left behind by a previous graceful shutdown.

**Resolution:**

- **Check ingester logs** for the wrapped error in `<details>` to identify the specific underlying cause.
- **Verify filesystem permissions** on the ingester's data directory, since the shutdown marker is a file on local disk.
- **Restart the ingester** after addressing the underlying issue.

**Properties:**

- Enforced by: Ingester startup
- Retryable: No (configuration or infrastructure must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Failed to start partition ring lifecycler

**Error message:**

```text
failed to start partition ring lifecycler: <details>
```

**Cause:**

The ingester could not start its Kafka partition ring lifecycler during startup. This is a separate component from the partition reader; it manages the ingester's membership in the partition ring. This only occurs when Kafka ingestion is enabled.

**Resolution:**

- **Check Kafka connectivity** from the ingester.
- **Verify the partition ring KV store** (the store used for the partition ring) is reachable.
- **Review ingester logs** for the wrapped error in `<details>`.
- **Check Kafka broker health** and partition availability.

**Properties:**

- Enforced by: Ingester startup
- Retryable: No (configuration or infrastructure must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Lifecycler failed

**Error message:**

```text
lifecycler failed: <details>
```

**Cause:**

The ingester's lifecycler (which manages ring membership) encountered a fatal error. This prevents the ingester from participating in the ring.

{{< admonition type="note" >}}
The identical message `lifecycler failed: <details>` is also emitted by the pattern ingester and by the ingest-limits service, not only the main ingester. Check the component name in the surrounding log entry to confirm which service raised it.
{{< /admonition >}}

**Resolution:**

- **Check KV store connectivity** (Consul, etcd, or memberlist).
- **Review ingester logs** for the specific lifecycler error.
- **Verify ring configuration** is consistent across all ingesters.
- **Restart the ingester** after fixing the underlying issue.

**Properties:**

- Enforced by: Ingester lifecycler
- Retryable: Yes (after fix and restart)
- HTTP status: N/A (internal failure)
- Configurable per tenant: No

## Pattern ingester errors

Pattern ingester errors occur when using the pattern ingester for automatic log pattern detection.

### Error: Pattern ingester replication factor must be 1

**Error message:**

```text
pattern ingester replication factor must be 1
```

**Cause:**

The pattern ingester is configured with a replication factor other than 1. Currently, the pattern ingester only supports a replication factor of 1.

**Resolution:**

- **Set the replication factor to 1**:

  ```yaml
  pattern_ingester:
    lifecycler:
      ring:
        replication_factor: 1
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Pattern ingester retain-for too short

**Error message:**

```text
retain-for (<duration>) must be greater than or equal to chunk-duration (<duration>)
```

**Cause:**

The pattern ingester's `retain_for` duration is shorter than `max_chunk_age`, which would cause data loss.

**Resolution:**

- **Increase the retain-for duration** to be at least as long as `max_chunk_age`:

  ```yaml
  pattern_ingester:
    retain_for: 15m   # Must be >= max_chunk_age
    max_chunk_age: 5m
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Pattern ingester chunk-duration too short

**Error message:**

```text
chunk-duration (<duration>) must be greater than or equal to sample-interval (<duration>)
```

**Cause:**

The pattern ingester's `max_chunk_age` is shorter than `pattern_sample_interval`. Chunks must span at least one sample interval to hold any data.

**Resolution:**

- **Increase `max_chunk_age`** to be at least as long as `pattern_sample_interval`:

  ```yaml
  pattern_ingester:
    max_chunk_age: 1h     # Must be >= pattern_sample_interval (default: 1h)
    pattern_sample_interval: 10s  # default: 10s
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

### Error: Pattern ingester volume threshold out of range

**Error message:**

```text
volume_threshold (<value>) must be between 0 and 1
```

**Cause:**

The `volume_threshold` value is outside the valid range of 0 to 1. This setting controls what fraction of log volume the pattern ingester tracks — only patterns representing the top X% of log volume are persisted.

**Resolution:**

- **Set `volume_threshold` to a value between 0 and 1** (default is `0.99`):

  ```yaml
  pattern_ingester:
    volume_threshold: 0.99
  ```

**Properties:**

- Enforced by: Configuration validation
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No

## API parameter errors

These errors occur when API requests contain invalid parameters.

### Error: Invalid direction

**Error message:**

```text
invalid direction '<value>'
```

**Cause:**

The `direction` query parameter contains an invalid value.

**Resolution:**

- **Use a valid direction value**:
  - `forward` - Oldest to newest
  - `backward` - Newest to oldest (default)

  ```bash
  curl "http://loki:3100/loki/api/v1/query_range?query={job=\"app\"}&direction=forward"
  ```

**Properties:**

- Enforced by: API handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Limit must be a positive value

**Error message:**

```text
limit must be a positive value
```

**Cause:**

The `limit` parameter is zero or negative. This same check applies to the `line_limit` and `detected-fields limit` parameters on the relevant endpoints, with equivalent messages. A sibling error, `limit value <value> is out of range [0, <max>]` (and the equivalent `line_limit value <value> is out of range [0, <max>]`), fires when the value is positive but exceeds the maximum allowed.

**Resolution:**

- **Provide a positive limit** within the allowed range:

  ```bash
  curl "http://loki:3100/loki/api/v1/query_range?query={job=\"app\"}&limit=100"
  ```

**Properties:**

- Enforced by: API handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Invalid query resolution step or interval

**Error messages:**

```text
zero or negative query resolution step widths are not accepted. Try a positive integer
exceeded maximum resolution of 11,000 points per time series. Try increasing the value of the step parameter
interval must be >= 0
```

**Cause:**

The `step` parameter on a range or instant metric query is zero, negative, or so small relative to the time range that it would produce more than 11,000 points per series. The `interval` parameter error fires when a negative interval is supplied.

**Resolution:**

- **Use a positive `step` value** sized to your time range (for example, a 1-hour range with 1-second steps would exceed the resolution limit).
- **Increase `step`** if you see the resolution-exceeded message.
- **Use a non-negative `interval`.**

**Properties:**

- Enforced by: API handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: End timestamp must not be before or equal to start time

**Error message:**

```text
end timestamp must not be before or equal to start time
```

**Cause:**

The query's `end` time is before or equal to its `start` time.

**Resolution:**

- **Ensure end time is after start time**:

  ```bash
  curl "http://loki:3100/loki/api/v1/query_range?\
  query={job=\"app\"}&\
  start=2024-01-01T00:00:00Z&\
  end=2024-01-02T00:00:00Z"
  ```

**Properties:**

- Enforced by: API handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Delay for tailing too large

**Error message:**

```text
delay_for can't be greater than <max>
```

**Cause:**

The `delay_for` parameter for tailing queries exceeds the maximum allowed value.

**Resolution:**

- **Reduce the delay_for value**:

  ```bash
  curl "http://loki:3100/loki/api/v1/tail?query={job=\"app\"}&delay_for=5"
  ```

  The maximum value is a fixed constant of 5 seconds; it isn't configurable.

**Properties:**

- Enforced by: API handler
- Retryable: No (request must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Query filtering requires compactor address

**Error message:**

```text
query filtering for deletes requires 'compactor_grpc_address' or 'compactor_address' to be configured
```

**Cause:**

Query-time filtering for delete requests is enabled but Loki doesn't know how to reach the compactor to retrieve active delete requests.

**Resolution:**

- **Configure the compactor address**:

  ```yaml
  compactor:
    compactor_grpc_address: compactor:9095
  ```

- **Or use the HTTP address**:

  ```yaml
  compactor:
    compactor_address: http://compactor:3100
  ```

**Properties:**

- Enforced by: Module initialization
- Retryable: No (configuration must be fixed)
- HTTP status: N/A (startup failure)
- Configurable per tenant: No
