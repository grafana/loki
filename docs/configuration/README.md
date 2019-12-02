# Configuring Loki

Loki is configured in a YAML file (usually referred to as `loki.yaml`)
which contains information on the Loki server and its individual components,
depending on which mode Loki is launched in.

Configuration examples can be found in the [Configuration Examples](examples.md) document.

* [Configuration File Reference](#configuration-file-reference)
* [server_config](#server_config)
* [distributor_config](#distributor_config)
* [querier_config](#querier_config)
* [ingester_client_config](#ingester_client_config)
  * [grpc_client_config](#grpc_client_config)
* [ingester_config](#ingester_config)
  * [lifecycler_config](#lifecycler_config)
  * [ring_config](#ring_config)
* [storage_config](#storage_config)
  * [cache_config](#cache_config)
* [chunk_store_config](#chunk_store_config)
* [schema_config](#schema_config)
  * [period_config](#period_config)
* [limits_config](#limits_config)
* [table_manager_config](#table_manager_config)
  * [provision_config](#provision_config)
    * [auto_scaling_config](#auto_scaling_config)

## Configuration File Reference

To specify which configuration file to load, pass the `-config.file` flag at the
command line. The file is written in [YAML format](https://en.wikipedia.org/wiki/YAML),
defined by the scheme below. Brackets indicate that a parameter is optional. For
non-list parameters the value is set to the specified default.

Generic placeholders are defined as follows:

* `<boolean>`: a boolean that can take the values `true` or `false`
* `<int>`: any integer matching the regular expression `[1-9]+[0-9]*`
* `<duration>`: a duration matching the regular expression `[0-9]+(ms|[smhdwy])`
* `<labelname>`: a string matching the regular expression `[a-zA-Z_][a-zA-Z0-9_]*`
* `<labelvalue>`: a string of unicode characters
* `<filename>`: a valid path relative to current working directory or an
    absolute path.
* `<host>`: a valid string consisting of a hostname or IP followed by an optional port number
* `<string>`: a regular string
* `<secret>`: a regular string that is a secret, such as a password

Supported contents and default values of `loki.yaml`:

```yaml
# The module to run Loki with. Supported values
# all, querier, table-manager, ingester, distributor
[target: <string> | default = "all"]

# Enables authentication through the X-Scope-OrgID header, which must be present
# if true. If false, the OrgID will always be set to "fake".
[auth_enabled: <boolean> | default = true]

# Configures the server of the launched module(s).
[server: <server_config>]

# Configures the distributor.
[distributor: <distributor_config>]

# Configures the querier. Only appropriate when running all modules or
# just the querier.
[querier: <querier_config>]

# Configures how the distributor will connect to ingesters. Only appropriate
# when running all modules, the distributor, or the querier.
[ingester_client: <ingester_client_config>]

# Configures the ingester and how the ingester will register itself to a
# key value store.
[ingester: <ingester_config>]

# Configures where Loki will store data.
[storage_config: <storage_config>]

# Configures how Loki will store data in the specific store.
[chunk_store_config: <chunk_store_config>]

# Configures the chunk index schema and where it is stored.
[schema_config: <schema_config>]

# Configures limits per-tenant or globally
[limits_config: <limits_config>]

# Configures the table manager for retention
[table_manager: <table_manager_config>]
```

## server_config

The `server_config` block configures Promtail's behavior as an HTTP server:

```yaml
# HTTP server listen host
[http_listen_host: <string>]

# HTTP server listen port
[http_listen_port: <int> | default = 80]

# gRPC server listen host
[grpc_listen_host: <string>]

# gRPC server listen port
[grpc_listen_port: <int> | default = 9095]

# Register instrumentation handlers (/metrics, etc.)
[register_instrumentation: <boolean> | default = true]

# Timeout for graceful shutdowns
[graceful_shutdown_timeout: <duration> | default = 30s]

# Read timeout for HTTP server
[http_server_read_timeout: <duration> | default = 30s]

# Write timeout for HTTP server
[http_server_write_timeout: <duration> | default = 30s]

# Idle timeout for HTTP server
[http_server_idle_timeout: <duration> | default = 120s]

# Max gRPC message size that can be received
[grpc_server_max_recv_msg_size: <int> | default = 4194304]

# Max gRPC message size that can be sent
[grpc_server_max_send_msg_size: <int> | default = 4194304]

# Limit on the number of concurrent streams for gRPC calls (0 = unlimited)
[grpc_server_max_concurrent_streams: <int> | default = 100]

# Log only messages with the given severity or above. Supported values [debug,
# info, warn, error]
[log_level: <string> | default = "info"]

# Base path to server all API routes from (e.g., /v1/).
[http_path_prefix: <string>]
```

## distributor_config

The `distributor_config` block configures the Loki Distributor.

```yaml
# Period at which to reload user ingestion limits.
[limiter_reload_period: <duration> | default = 5m]
```

## querier_config

The `querier_config` block configures the Loki Querier.

```yaml
# Timeout when querying ingesters or storage during the execution of a
# query request.
[query_timeout: <duration> | default = 1m]

# Limit of the duration for which live tailing requests should be
# served.
[tail_max_duration: <duration> | default = 1h]

# Configuration options for the LogQL engine.
engine:
  # Timeout for query execution
  [timeout: <duration> | default = 3m]

  # The maximum amount of time to look back for log lines. Only
  # applicable for instant log queries.
  [max_look_back_period: <duration> | default = 30s]
```

## ingester_client_config

The `ingester_client_config` block configures how connections to ingesters
operate.

```yaml
# Configures how connections are pooled
pool_config:
  # Whether or not to do health checks.
  [health_check_ingesters: <boolean> | default = false]

  # How frequently to clean up clients for servers that have gone away after
  # a health check.
  [client_cleanup_period: <duration> | default = 15s]

  # How quickly a dead client will be removed after it has been detected
  # to disappear. Set this to a value to allow time for a secondary
  # health check to recover the missing client.
  [remotetimeout: <duration>]

# The remote request timeout on the client side.
[remote_timeout: <duration> | default = 5s]

# Configures how the gRPC connection to ingesters work as a
# client.
[grpc_client_config: <grpc_client_config>]
```

### grpc_client_config

The `grpc_client_config` block configures a client connection to a gRPC service.

```yaml
# The maximum size in bytes the client can recieve
[max_recv_msg_size: <int> | default = 104857600]

# The maximum size in bytes the client can send
[max_send_msg_size: <int> | default = 16777216]

# Whether or not messages should be compressed
[use_gzip_compression: <bool> | default = false]

# Rate limit for gRPC client. 0 is disabled
[rate_limit: <float> | default = 0]

# Rate limit burst for gRPC client.
[rate_limit_burst: <int> | default = 0]

# Enable backoff and retry when a rate limit is hit.
[backoff_on_ratelimits: <bool> | default = false]

# Configures backoff when enbaled.
backoff_config:
  # Minimum delay when backing off.
  [minbackoff: <duration> | default = 100ms]

  # The maximum delay when backing off.
  [maxbackoff: <duration> | default = 10s]

  # Number of times to backoff and retry before failing.
  [maxretries: <int> | default = 10]
```

## ingester_config

The `ingester_config` block configures Ingesters.

```yaml
# Configures how the lifecycle of the ingester will operate
# and where it will register for discovery.
[lifecycler: <lifecycler_config>]

# Number of times to try and transfer chunks when leaving before
# falling back to flushing to the store.
[max_transfer_retries: <int> | default = 10]

# How many flushes can happen concurrently from each stream.
[concurrent_flushes: <int> | default = 16]

# How often should the ingester see if there are any blocks
# to flush
[flush_check_period: <duration> | default = 30s]

# The timeout before a flush is cancelled
[flush_op_timeout: <duration> | default = 10s]

# How long chunks should be retained in-memory after they've
# been flushed.
[chunk_retain_period: <duration> | default = 15m]

# How long chunks should sit in-memory with no updates before
# being flushed if they don't hit the max block size. This means
# that half-empty chunks will still be flushed after a certain
# period as long as they receieve no further activity.
[chunk_idle_period: <duration> | default = 30m]

# The maximum size in bytes a chunk can be before it should be flushed.
[chunk_block_size: <int> | default = 262144]
```

### lifecycler_config

The `lifecycler_config` is used by the Ingester to control how that ingester
registers itself into the ring and manages its lifecycle during its stay in the
ring.

```yaml
# Configures the ring the lifecycler connects to
[ring: <ring_config>]

# The number of tokens the lifecycler will generate and put into the ring if
# it joined without transfering tokens from another lifecycler.
[num_tokens: <int> | default = 128]

# Period at which to heartbeat to the underlying ring.
[heartbeat_period: <duration> | default = 5s]

# How long to wait to claim tokens and chunks from another member when
# that member is leaving. Will join automatically after the duration expires.
[join_after: <duration> | default = 0s]

# Minimum duration to wait before becoming ready. This is to work around race
# conditions with ingesters exiting and updating the ring.
[min_ready_duration: <duration> | default = 1m]

# Store tokens in a normalised fashion to reduce the number of allocations.
[normalise_tokens: <boolean> | default = false]

# Name of network interfaces to read addresses from.
interface_names:
  - [<string> ... | default = ["eth0", "en0"]]

# Duration to sleep before exiting to ensure metrics are scraped.
[final_sleep: <duration> | default = 30s]
```

### ring_config

The `ring_config` is used to discover and connect to Ingesters.

```yaml
kvstore:
  # The backend storage to use for the ring. Supported values are
  # consul, etcd, inmemory
  store: <string>

  # The prefix for the keys in the store. Should end with a /.
  [prefix: <string> | default = "collectors/"]

  # Configuration for a Consul client. Only applies if store
  # is "consul"
  consul:
    # The hostname and port of Consul.
    [host: <string> | duration = "localhost:8500"]

    # The ACL Token used to interact with Consul.
    [acltoken: <string>]

    # The HTTP timeout when communicating with Consul
    [httpclienttimeout: <duration> | default = 20s]

    # Whether or not consistent reads to Consul are enabled.
    [consistentreads: <boolean> | default = true]

  # Configuration for an ETCD v3 client. Only applies if
  # store is "etcd"
  etcd:
    # The ETCD endpoints to connect to.
    endpoints:
      - <string>

    # The Dial timeout for the ETCD connection.
    [dial_tmeout: <duration> | default = 10s]

    # The maximum number of retries to do for failed ops to ETCD.
    [max_retries: <int> | default = 10]

# The heartbeart timeout after which ingesters are skipped for
# reading and writing.
[heartbeart_timeout: <duration> | default = 1m]

# The number of ingesters to write to and read from. Must be at least
# 1.
[replication_factor: <int> | default = 3]
```

## storage_config

The `storage_config` block configures one of many possible stores for both the
index and chunks. Which configuration is read from depends on the schema_config
block and what is set for the store value.

```yaml
# Configures storing chunks in AWS. Required options only required when aws is
# present.
aws:
  # S3 or S3-compatible URL to connect to. If only region is specified as a
  # host, the proper endpoint will be deduced. Use inmemory:///<bucket-name> to
  # use a mock in-memory implementation.
  s3: <string>

  # Set to true to force the request to use path-style addressing
  [s3forcepathstyle: <boolean> | default = false]

  # Configure the DynamoDB conection
  dynamodbconfig:
    # URL for DynamoDB with escaped Key and Secret encoded. If only region is specified as a
    # host, the proper endpoint will be deduced. Use inmemory:///<bucket-name> to
    # use a mock in-memory implementation.
    dynamodb: <string>

    # DynamoDB table management requests per-second limit.
    [apilimit: <float> | default = 2.0]

    # DynamoDB rate cap to back off when throttled.
    [throttlelimit: <float> | default = 10.0]

    # Application Autoscaling endpoint URL with escaped Key and Secret
    # encoded.
    [applicationautoscaling: <string>]

    # Metics-based autoscaling configuration.
    metrics:
      # Use metrics-based autoscaling via this Prometheus query URL.
      [url: <string>]

      # Queue length above which we will scale up capacity.
      [targetqueuelen: <int> | default = 100000]

      # Scale up capacity by this multiple
      [scaleupfactor: <float64> | default = 1.3]

      # Ignore throttling below this level (rate per second)
      [minthrottling: <float64> | default = 1]

      # Query to fetch ingester queue length
      [queuelengthquery: <string> | default = "sum(avg_over_time(cortex_ingester_flush_queue_length{job="cortex/ingester"}[2m]))"]

      # Query to fetch throttle rates per table
      [throttlequery: <string> | default = "sum(rate(cortex_dynamo_throttled_total{operation="DynamoDB.BatchWriteItem"}[1m])) by (table) > 0"]

      # Quer to fetch write capacity usage per table
      [usagequery: <string> | default = "sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.BatchWriteItem"}[15m])) by (table) > 0"]

      # Query to fetch read capacity usage per table
      [readusagequery: <string> | default = "sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.QueryPages"}[1h])) by (table) > 0"]

      # Query to fetch read errors per table
      [readerrorquery: <string> | default = "sum(increase(cortex_dynamo_failures_total{operation="DynamoDB.QueryPages",error="ProvisionedThroughputExceededException"}[1m])) by (table) > 0"]

    # Number of chunks to group together to parallelise fetches (0 to disable)
    [chunkgangsize: <int> | default = 10]

    # Max number of chunk get operations to start in parallel.
    [chunkgetmaxparallelism: <int> | default = 32]

# Configures storing chunks in Bigtable. Required fields only required
# when bigtable is defined in config.
bigtable:
  # BigTable project ID
  project: <string>

  # BigTable instance ID
  instance: <string>

  # Configures the gRPC client used to connect to Bigtable.
  [grpc_client_config: <grpc_client_config>]

# Configures storing index in GCS. Required fields only required
# when gcs is defined in config.
gcs:
  # Name of GCS bucket to put chunks in.
  bucket_name: <string>

  # The size of the buffer that the GCS client uses for each PUT request. 0
  # to disable buffering.
  [chunk_buffer_size: <int> | default = 0]

  # The duration after which the requests to GCS should be timed out.
  [request_timeout: <duration> | default = 0s]

# Configures storing chunks in Cassandra
cassandra:
  # Comma-separated hostnames or IPs of Cassandra instances
  addresses: <string>

  # Port that cassandra is running on
  [port: <int> | default = 9042]

  # Keyspace to use in Cassandra
  keyspace: <string>

  # Consistency level for Cassandra
  [consistency: <string> | default = "QUORUM"]

  # Replication factor to use in Cassandra.
  [replication_factor: <int> | default = 1]

  # Instruct the Cassandra driver to not attempt to get host
  # info from the system.peers table.
  [disable_initial_host_lookup: <bool> | default = false]

  # Use SSL when connecting to Cassandra instances.
  [SSL: <boolean> | default = false]

  # Require SSL certificate validation when SSL is enabled.
  [host_verification: <bool> | default = true]

  # Path to certificate file to verify the peer when SSL is
  # enabled.
  [CA_path: <string>]

  # Enable password authentication when connecting to Cassandra.
  [auth: <bool> | default = false]

  # Username for password authentication when auth is true.
  [username: <string>]

  # Password for password authentication when auth is true.
  [password: <string>]

  # Timeout when connecting to Cassandra.
  [timeout: <duration> | default = 600ms]

  # Initial connection timeout during initial dial to server.
  [connect_timeout: <duration> | default = 600ms]

# Configures storing index in BoltDB. Required fields only
# required when boltdb is present in config.
boltdb:
  # Location of BoltDB index files.
  directory: <string>

# Configures storing the chunks on the local filesystem. Required
# fields only required when filesystem is present in config.
filesystem:
  # Directory to store chunks in.
  directory: <string>

# Cache validity for active index entries. Should be no higher than
# the chunk_idle_period in the ingester settings.
[indexcachevalidity: <duration> | default = 5m]

# The maximum number of chunks to fetch per batch.
[max_chunk_batch_size: <int> | default = 50]

# Config for how the cache for index queries should
# be built.
index_queries_cache_config: <cache_config>
```

### cache_config

The `cache_config` block configures how Loki will cache requests, chunks, and
the index to a backing cache store.

```yaml
# Enable in-memory cache.
[enable_fifocache: <boolean>]

# The default validity of entries for caches unless overriden.
# "defaul" is correct.
[defaul_validity: <duration>]

# Configures the background cache when memcached is used.
background:
  # How many goroutines to use to write back to memcached.
  [writeback_goroutines: <int> | default = 10]

  # How many chunks to buffer for background write back to memcached.
  [writeback_buffer: <int> = 10000]

# Configures memcached settings.
memcached:
  # Configures how long keys stay in memcached.
  expiration: <duration>

  # Configures how many keys to fetch in each batch request.
  batch_size: <int>

  # Maximum active requests to memcached.
  [parallelism: <int> | default = 100]

# Configures how to connect to one or more memcached servers.
memcached_client:
  # The hostname to use for memcached services when caching chunks. If
  # empty, no memcached will be used. A SRV lookup will be used.
  [host: <string>]

  # SRV service used to discover memcached servers.
  [service: <string> | default = "memcached"]

  # Maximum time to wait before giving up on memcached requests.
  [timeout: <duration> | default = 100ms]

  # The maximum number of idle connections in the memcached client
  # pool.
  [max_idle_conns: <int> | default = 100]

  # The period with which to poll the DNS for memcached servers.
  [update_interval: <duration> | default = 1m]

  # Whether or not to use a consistent hash to discover multiple memcached
  # servers.
  [consistent_hash: <bool>]

fifocache:
  # Number of entries to cache in-memory.
  [size: <int> | default = 0]

  # The expiry duration for the in-memory cache.
  [validity: <duration> | default = 0s]
```

## chunk_store_config

The `chunk_store_config` block configures how chunks will be cached and how long
to wait before saving them to the backing store.

```yaml
# The cache configuration for storing chunks
[chunk_cache_config: <cache_config>]

# The cache configuration for deduplicating writes
[write_dedupe_cache_config: <cache_config>]

# The minimum time between a chunk update and being saved
# to the store.
[min_chunk_age: <duration>]

# Cache index entries older than this period. Default is
# disabled.
[cache_lookups_older_than: <duration>]

# Limit how long back data can be queries. Default is disabled.
[max_look_back_period: <duration>]
```

## schema_config

The `schema_config` block configures schemas from given dates.

```yaml
# The configuration for chunk index schemas.
configs:
  - [<period_config>]
```

### period_config

The `period_config` block configures what index schemas should be used
for from specific time periods.

```yaml
# The date of the first day that index buckets should be created. Use
# a date in the past if this is your only period_config, otherwise
# use a date when you want the schema to switch over.
[from: <daytime>]

# store and object_store below affect which <storage_config> key is
# used.

# Which store to use for the index. Either cassandra, bigtable, dynamodb, or
# boltdb
store: <string>

# Which store to use for the chunks. Either gcs, s3, inmemory, filesystem,
# cassandra. If omitted, defaults to same value as store.
[object_store: <string>]

# The schema to use. Set to v9 or v10.
schema: <string>

# Configures how the index is updated and stored.
index:
  # Table prefix for all period tables.
  prefix: <string>
  # Table period.
  [period: <duration> | default = 168h]
  # A map to be added to all managed tables.
  tags:
    [<string>: <string> ...]

# Configured how the chunks are updated and stored.
chunks:
  # Table prefix for all period tables.
  prefix: <string>
  # Table period.
  [period: <duration> | default = 168h]
  # A map to be added to all managed tables.
  tags:
    [<string>: <string> ...]

# How many shards will be created. Only used if schema is v10.
[row_shards: <int> | default = 16]
```

Where `daytime` is a value in the format of `yyyy-mm-dd` like `2006-01-02`.

## limits_config

The `limits_config` block configures global and per-tenant limits for ingesting
logs in Loki.

```yaml
# Per-user ingestion rate limit in sample size per second. Units in MB.
[ingestion_rate_mb: <float> | default = 4]

# Per-user allowed ingestion burst size (in sample size). Units in MB. Warning,
# very high limits will be reset every limiter_reload_period defined in
# distributor_config.
[ingestion_burst_size_mb: <int> | default = 6]

# Maximum length of a label name.
[max_label_name_length: <int> | default = 1024]

# Maximum length of a label value.
[max_label_value_length: <int> | default = 2048]

# Maximum number of label names per series.
[max_label_names_per_series: <int> | default = 30]

# Whether or not old samples will be rejected.
[reject_old_samples: <bool> | default = false]

# Maximum accepted sample age before rejecting.
[reject_old_samples_max_age: <duration> | default = 336h]

# Duration for a table to be created/deleted before/after it's
# needed. Samples won't be accepted before this time.
[creation_grace_period: <duration> | default = 10m]

# Enforce every sample has a metric name.
[enforce_metric_name: <boolean> | default = true]

# Maximum number of active streams per user.
[max_streams_per_user: <int> | default = 10e3]

# Maximum number of chunks that can be fetched by a single query.
[max_chunks_per_query: <int> | default = 2000000]

# The limit to length of chunk store queries. 0 to disable.
[max_query_length: <duration> | default = 0]

# Maximum number of queries that will be scheduled in parallel by the
# frontend.
[max_query_parallelism: <int> | default = 14]

# Cardinality limit for index queries
[cardinality_limit: <int> | default = 100000]

# Maximum number of stream matchers per query.
[max_streams_matchers_per_query: <int> | default = 1000]

# Filename of per-user overrides file
[per_tenant_override_config: <string>]

# Period with which to reload the overrides file if configured.
[per_tenant_override_period: <duration> | default = 10s]
```

## table_manager_config

The `table_manager_config` block configures how the table manager operates
and how to provision tables when DynamoDB is used as the backing store.

```yaml
# Master 'off-switch' for table capacity updates, e.g. when troubleshooting
[throughput_updates_disabled: <boolean> | default = false]

# Master 'on-switch' for table retention deletions
[retention_deletes_enabled: <boolean> | default = false]

# How far back tables will be kept before they are deleted. 0s disables
# deletion. The retention period must be a multiple of the index / chunks
# table "period" (see period_config).
[retention_period: <duration> | default = 0s]

# Period with which the table manager will poll for tables.
[dynamodb_poll_interval: <duration> | default = 2m]

# duration a table will be created before it is needed.
[creation_grace_period: <duration> | default = 10m]

# Configures management of the index tables for DynamoDB.
index_tables_provisioning: <provision_config>

# Configures management of the chunk tables for DynamoDB.
chunk_tables_provisioning: <provision_config>
```

### provision_config

The `provision_config` block configures provisioning capacity for DynamoDB.

```yaml
# Enables on-demand throughput provisioning for the storage
# provider, if supported. Applies only to tables which are not autoscaled.
[provisioned_throughput_on_demand_mode: <boolean> | default = false]

# DynamoDB table default write throughput.
[provisioned_write_throughput: <int> | default = 3000]

# DynamoDB table default read throughput.
[provisioned_read_throughput: <int> | default = 300]

# Enables on-demand throughput provisioning for the storage provide,
# if supported. Applies only to tables which are not autoscaled.
[inactive_throughput_on_demand_mode: <boolean> | default = false]

# DynamoDB table write throughput for inactive tables.
[inactive_write_throughput: <int> | default = 1]

# DynamoDB table read throughput for inactive tables.
[inactive_read_throughput: <int> | Default = 300]

# Active table write autoscale config.
[write_scale: <auto_scaling_config>]

# Inactive table write autoscale config.
[inactive_write_scale: <auto_scaling_config>]

# Number of last inactive tables to enable write autoscale.
[inactive_write_scale_lastn: <int>]

# Active table read autoscale config.
[read_scale: <auto_scaling_config>]

# Inactive table read autoscale config.
[inactive_read_scale: <auto_scaling_config>]

# Number of last inactive tables to enable read autoscale.
[inactive_read_scale_lastn: <int>]
```

#### auto_scaling_config

The `auto_scaling_config` block configures autoscaling for DynamoDB.

```yaml
# Whether or not autoscaling should be enabled.
[enabled: <boolean>: default = false]

# AWS AutoScaling role ARN
[role_arn: <string>]

# DynamoDB minimum provision capacity.
[min_capacity: <int> | default = 3000]

# DynamoDB maximum provision capacity.
[max_capacity: <int> | default = 6000]

# DynamoDB minimum seconds between each autoscale up.
[out_cooldown: <int> | default = 1800]

# DynamoDB minimum seconds between each autoscale down.
[in_cooldown: <int> | default = 1800]

# DynamoDB target ratio of consumed capacity to provisioned capacity.
[target: <float> | default = 80]
```
