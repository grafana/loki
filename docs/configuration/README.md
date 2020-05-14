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
* [frontend_worker_config](#frontend_worker_config)
* [table_manager_config](#table_manager_config)
  * [provision_config](#provision_config)
    * [auto_scaling_config](#auto_scaling_config)
* [Runtime Configuration file](#runtime-configuration-file)

## Configuration File Reference

To specify which configuration file to load, pass the `-config.file` flag at the
command line. The file is written in [YAML format](https://en.wikipedia.org/wiki/YAML),
defined by the scheme below. Brackets indicate that a parameter is optional. For
non-list parameters the value is set to the specified default.

Generic placeholders are defined as follows:

* `<boolean>`: a boolean that can take the values `true` or `false`
* `<int>`: any integer matching the regular expression `[1-9]+[0-9]*`
* `<duration>`: a duration matching the regular expression `[0-9]+(ns|us|µs|ms|[smh])`
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

# The frontend_worker_config configures the worker - running within the Loki
# querier - picking up and executing queries enqueued by the query-frontend.
[frontend_worker: <frontend_worker_config>]

# Configures the table manager for retention
[table_manager: <table_manager_config>]

# Configuration for "runtime config" module, responsible for reloading runtime configuration file.
[runtime_config: <runtime_config>]
```

## server_config

The `server_config` block configures Promtail's behavior as an HTTP server:

```yaml
# HTTP server listen host
[http_listen_address: <string>]

# HTTP server listen port
[http_listen_port: <int> | default = 80]

# gRPC server listen host
[grpc_listen_address: <string>]

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
# Configures the distributors ring, used when the "global" ingestion rate
# strategy is enabled.
[ring: <ring_config>]
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

# Time to wait before sending more than the minimum successful query
# requests.
[extra_query_delay: <duration> | default = 0s]

# Maximum lookback beyond which queries are not sent to ingester.
# 0 means all queries are sent to ingester.
[query_ingesters_within: <duration> | default = 0s]

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
# The maximum size in bytes the client can receive
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

# Configures backoff when enabled.
backoff_config:
  # Minimum delay when backing off.
  [min_period: <duration> | default = 100ms]

  # The maximum delay when backing off.
  [max_period: <duration> | default = 10s]

  # Number of times to backoff and retry before failing.
  [max_retries: <int> | default = 10]
```

## ingester_config

The `ingester_config` block configures Ingesters.

```yaml
# Configures how the lifecycle of the ingester will operate
# and where it will register for discovery.
[lifecycler: <lifecycler_config>]

# Number of times to try and transfer chunks when leaving before
# falling back to flushing to the store. Zero = no transfers are done.
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
# period as long as they receive no further activity.
[chunk_idle_period: <duration> | default = 30m]

# The targeted _uncompressed_ size in bytes of a chunk block
# When this threshold is exceeded the head block will be cut and compressed inside the chunk
[chunk_block_size: <int> | default = 262144]

# A target _compressed_ size in bytes for chunks.
# This is a desired size not an exact size, chunks may be slightly bigger
# or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period)
# The default value of 0 for this will create chunks with a fixed 10 blocks,
# A non zero value will create chunks with a variable number of blocks to meet the target size.
[chunk_target_size: <int> | default = 0]

# The compression algorithm to use for chunks. (supported: gzip, lz4, snappy)
# You should choose your algorithm depending on your need:
# - `gzip` highest compression ratio but also slowest decompression speed. (144 kB per chunk)
# - `lz4` fastest compression speed (188 kB per chunk)
# - `snappy` fast and popular compression algorithm (272 kB per chunk)
[chunk_encoding: <string> | default = gzip]

# Parameters used to synchronize ingesters to cut chunks at the same moment.
# Sync period is used to roll over incoming entry to a new chunk. If chunk's utilization
# isn't high enough (eg. less than 50% when sync_min_utilization is set to 0.5), then
# this chunk rollover doesn't happen.
[sync_period: <duration> | default = 0]
[sync_min_utilization: <float> | Default = 0]

# The maximum number of errors a stream will report to the user
# when a push fails. 0 to make unlimited.
[max_returned_stream_errors: <int> | default = 10]

# The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this the current chunk will be flushed to the store and a new chunk created.
[max_chunk_age: <duration> | default = 1h]

# How far in the past an ingester is allowed to query the store for data.  
# This is only useful for running multiple loki binaries with a shared ring with a `filesystem` store which is NOT shared between the binaries
# When using any "shared" object store like S3 or GCS this value must always be left as 0
# It is an error to configure this to a non-zero value when using any object store other than `filesystem`
# Use a value of -1 to allow the ingester to query the store infinitely far back in time.
[query_store_max_look_back_period: <duration> | default = 0]

```

### lifecycler_config

The `lifecycler_config` is used by the Ingester to control how that ingester
registers itself into the ring and manages its lifecycle during its stay in the
ring.

```yaml
# Configures the ring the lifecycler connects to
[ring: <ring_config>]

# The number of tokens the lifecycler will generate and put into the ring if
# it joined without transferring tokens from another lifecycler.
[num_tokens: <int> | default = 128]

# Period at which to heartbeat to the underlying ring.
[heartbeat_period: <duration> | default = 5s]

# How long to wait to claim tokens and chunks from another member when
# that member is leaving. Will join automatically after the duration expires.
[join_after: <duration> | default = 0s]

# Minimum duration to wait before becoming ready. This is to work around race
# conditions with ingesters exiting and updating the ring.
[min_ready_duration: <duration> | default = 1m]

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
    [acl_token: <string>]

    # The HTTP timeout when communicating with Consul
    [http_client_timeout: <duration> | default = 20s]

    # Whether or not consistent reads to Consul are enabled.
    [consistent_reads: <boolean> | default = true]

  # Configuration for an ETCD v3 client. Only applies if
  # store is "etcd"
  etcd:
    # The ETCD endpoints to connect to.
    endpoints:
      - <string>

    # The Dial timeout for the ETCD connection.
    [dial_timeout: <duration> | default = 10s]

    # The maximum number of retries to do for failed ops to ETCD.
    [max_retries: <int> | default = 10]

# The heartbeat timeout after which ingesters are skipped for
# reading and writing.
[heartbeat_timeout: <duration> | default = 1m]

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

  # Configure the DynamoDB connection
  dynamodb:
    # URL for DynamoDB with escaped Key and Secret encoded. If only region is specified as a
    # host, the proper endpoint will be deduced. Use inmemory:///<bucket-name> to
    # use a mock in-memory implementation.
    dynamodb_url: <string>

    # DynamoDB table management requests per-second limit.
    [api_limit: <float> | default = 2.0]

    # DynamoDB rate cap to back off when throttled.
    [throttle_limit: <float> | default = 10.0]

    # Metrics-based autoscaling configuration.
    metrics:
      # Use metrics-based autoscaling via this Prometheus query URL.
      [url: <string>]

      # Queue length above which we will scale up capacity.
      [target_queue_length: <int> | default = 100000]

      # Scale up capacity by this multiple
      [scale_up_factor: <float64> | default = 1.3]

      # Ignore throttling below this level (rate per second)
      [ignore_throttle_below: <float64> | default = 1]

      # Query to fetch ingester queue length
      [queue_length_query: <string> | default = "sum(avg_over_time(cortex_ingester_flush_queue_length{job="cortex/ingester"}[2m]))"]

      # Query to fetch throttle rates per table
      [write_throttle_query: <string> | default = "sum(rate(cortex_dynamo_throttled_total{operation="DynamoDB.BatchWriteItem"}[1m])) by (table) > 0"]

      # Query to fetch write capacity usage per table
      [write_usage_query: <string> | default = "sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.BatchWriteItem"}[15m])) by (table) > 0"]

      # Query to fetch read capacity usage per table
      [read_usage_query: <string> | default = "sum(rate(cortex_dynamo_consumed_capacity_total{operation="DynamoDB.QueryPages"}[1h])) by (table) > 0"]

      # Query to fetch read errors per table
      [read_error_query: <string> | default = "sum(increase(cortex_dynamo_failures_total{operation="DynamoDB.QueryPages",error="ProvisionedThroughputExceededException"}[1m])) by (table) > 0"]

    # Number of chunks to group together to parallelise fetches (0 to disable)
    [chunk_gang_size: <int> | default = 10]

    # Max number of chunk get operations to start in parallel.
    [chunk_get_max_parallelism: <int> | default = 32]

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

swift:
  # Openstack authentication URL.
  # CLI flag: -ruler.storage.swift.auth-url
  [auth_url: <string> | default = ""]

  # Openstack username for the api.
  # CLI flag: -ruler.storage.swift.username
  [username: <string> | default = ""]

  # Openstack user's domain name.
  # CLI flag: -ruler.storage.swift.user-domain-name
  [user_domain_name: <string> | default = ""]

  # Openstack user's domain id.
  # CLI flag: -ruler.storage.swift.user-domain-id
  [user_domain_id: <string> | default = ""]

  # Openstack userid for the api.
  # CLI flag: -ruler.storage.swift.user-id
  [user_id: <string> | default = ""]

  # Openstack api key.
  # CLI flag: -ruler.storage.swift.password
  [password: <string> | default = ""]

  # Openstack user's domain id.
  # CLI flag: -ruler.storage.swift.domain-id
  [domain_id: <string> | default = ""]

  # Openstack user's domain name.
  # CLI flag: -ruler.storage.swift.domain-name
  [domain_name: <string> | default = ""]

  # Openstack project id (v2,v3 auth only).
  # CLI flag: -ruler.storage.swift.project-id
  [project_id: <string> | default = ""]

  # Openstack project name (v2,v3 auth only).
  # CLI flag: -ruler.storage.swift.project-name
  [project_name: <string> | default = ""]

  # Id of the project's domain (v3 auth only), only needed if it differs the
  # from user domain.
  # CLI flag: -ruler.storage.swift.project-domain-id
  [project_domain_id: <string> | default = ""]

  # Name of the project's domain (v3 auth only), only needed if it differs
  # from the user domain.
  # CLI flag: -ruler.storage.swift.project-domain-name
  [project_domain_name: <string> | default = ""]

  # Openstack Region to use eg LON, ORD - default is use first region (v2,v3
  # auth only)
  # CLI flag: -ruler.storage.swift.region-name
  [region_name: <string> | default = ""]

  # Name of the Swift container to put chunks in.
  # CLI flag: -ruler.storage.swift.container-name
  [container_name: <string> | default = "cortex"]

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
[index_cache_validity: <duration> | default = 5m]

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

# The default validity of entries for caches unless overridden.
# NOTE In Loki versions older than 1.4.0 this was "defaul_validity".
[default_validity: <duration>]

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

redis:
  # Redis service endpoint to use when caching chunks. If empty, no redis will be used.
  [endpoint: <string>]
  # Maximum time to wait before giving up on redis requests.
  [timeout: <duration> | default = 100ms]
  # How long keys stay in the redis.
  [expiration: <duration> | default = 0s]
  # Maximum number of idle connections in pool.
  [max_idle_conns: <int> | default = 80]
  # Maximum number of active connections in pool.
  [max_active_conns: <int> | default = 0]
  # Password to use when connecting to redis.
  [password: <string>]
  # Enables connecting to redis with TLS.
  [enable_tls: <boolean> | default = false]

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

# Limit how long back data can be queried. Default is disabled.
# This should always be set to a value less than or equal to
# what is set in `table_manager.retention_period`.
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

# Which store to use for the index. Either aws, gcp, bigtable, bigtable-hashed,
# cassandra, or boltdb.
store: <string>

# Which store to use for the chunks. Either aws, aws-dynamo, azure, gcp,
# bigtable, gcs, cassandra, swift or filesystem. If omitted, defaults to the same
# value as store.
[object_store: <string>]

# The schema version to use, current recommended schema is v11.
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

# How many shards will be created. Only used if schema is v10 or greater.
[row_shards: <int> | default = 16]
```

Where `daytime` is a value in the format of `yyyy-mm-dd` like `2006-01-02`.

## limits_config

The `limits_config` block configures global and per-tenant limits for ingesting
logs in Loki.

```yaml
# Whether the ingestion rate limit should be applied individually to each
# distributor instance (local), or evenly shared across the cluster (global).
# The ingestion rate strategy cannot be overridden on a per-tenant basis.
#
# - local: enforces the limit on a per distributor basis. The actual effective
#   rate limit will be N times higher, where N is the number of distributor
#   replicas.
# - global: enforces the limit globally, configuring a per-distributor local
#   rate limiter as "ingestion_rate / N", where N is the number of distributor
#   replicas (it's automatically adjusted if the number of replicas change).
#   The global strategy requires the distributors to form their own ring, which
#   is used to keep track of the current number of healthy distributor replicas.
[ingestion_rate_strategy: <string> | default = "local"]

# Per-user ingestion rate limit in sample size per second. Units in MB.
[ingestion_rate_mb: <float> | default = 4]

# Per-user allowed ingestion burst size (in sample size). Units in MB.
# The burst size refers to the per-distributor local rate limiter even in the
# case of the "global" strategy, and should be set at least to the maximum logs
# size expected in a single push request.
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

# Maximum number of active streams per user, per ingester. 0 to disable.
[max_streams_per_user: <int> | default = 10000]

# Maximum line size on ingestion path. Example: 256kb.
# There is no limit when unset.
[max_line_size: <string> | default = none ]

# Maximum number of log entries that will be returned for a query. 0 to disable.
[max_entries_limit: <int> | default = 5000 ]

# Maximum number of active streams per user, across the cluster. 0 to disable.
# When the global limit is enabled, each ingester is configured with a dynamic
# local limit based on the replication factor and the current number of healthy
# ingesters, and is kept updated whenever the number of ingesters change.
[max_global_streams_per_user: <int> | default = 0]

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

# Feature renamed to 'runtime configuration', flag deprecated in favor of -runtime-config.file (runtime_config.file in YAML)
[per_tenant_override_config: <string>]

# Feature renamed to 'runtime configuration', flag deprecated in favor of -runtime-config.reload-period (runtime_config.period in YAML)
[per_tenant_override_period: <duration> | default = 10s]
```

### `frontend_worker_config`

The `frontend_worker_config` configures the worker - running within the Loki querier - picking up and executing queries enqueued by the query-frontend.

```yaml
# Address of query frontend service, in host:port format.
# CLI flag: -querier.frontend-address
[frontend_address: <string> | default = ""]

# Number of simultaneous queries to process.
# CLI flag: -querier.worker-parallelism
[parallelism: <int> | default = 10]

# How often to query DNS.
# CLI flag: -querier.dns-lookup-period
[dns_lookup_duration: <duration> | default = 10s]

grpc_client_config:
  # gRPC client max receive message size (bytes).
  # CLI flag: -querier.frontend-client.grpc-max-recv-msg-size
  [max_recv_msg_size: <int> | default = 104857600]

  # gRPC client max send message size (bytes).
  # CLI flag: -querier.frontend-client.grpc-max-send-msg-size
  [max_send_msg_size: <int> | default = 16777216]

  # Use compression when sending messages.
  # CLI flag: -querier.frontend-client.grpc-use-gzip-compression
  [use_gzip_compression: <boolean> | default = false]

  # Rate limit for gRPC client; 0 means disabled.
  # CLI flag: -querier.frontend-client.grpc-client-rate-limit
  [rate_limit: <float> | default = 0]

  # Rate limit burst for gRPC client.
  # CLI flag: -querier.frontend-client.grpc-client-rate-limit-burst
  [rate_limit_burst: <int> | default = 0]

  # Enable backoff and retry when we hit ratelimits.
  # CLI flag: -querier.frontend-client.backoff-on-ratelimits
  [backoff_on_ratelimits: <boolean> | default = false]

  backoff_config:
    # Minimum delay when backing off.
    # CLI flag: -querier.frontend-client.backoff-min-period
    [min_period: <duration> | default = 100ms]

    # Maximum delay when backing off.
    # CLI flag: -querier.frontend-client.backoff-max-period
    [max_period: <duration> | default = 10s]

    # Number of times to backoff and retry before failing.
    # CLI flag: -querier.frontend-client.backoff-retries
    [max_retries: <int> | default = 10]
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
[poll_interval: <duration> | default = 2m]

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
[enable_ondemand_throughput_mode: <boolean> | default = false]

# DynamoDB table default write throughput.
[provisioned_write_throughput: <int> | default = 3000]

# DynamoDB table default read throughput.
[provisioned_read_throughput: <int> | default = 300]

# Enables on-demand throughput provisioning for the storage provide,
# if supported. Applies only to tables which are not autoscaled.
[enable_inactive_throughput_on_demand_mode: <boolean> | default = false]

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

## Runtime Configuration file

Loki has a concept of "runtime config" file, which is simply a file that is reloaded while Loki is running. It is used by some Loki components to allow operator to change some aspects of Loki configuration without restarting it. File is specified by using `-runtime-config.file=<filename>` flag and reload period (which defaults to 10 seconds) can be changed by `-runtime-config.reload-period=<duration>` flag. Previously this mechanism was only used by limits overrides, and flags were called `-limits.per-user-override-config=<filename>` and `-limits.per-user-override-period=10s` respectively. These are still used, if `-runtime-config.file=<filename>` is not specified.

At the moment, two components use runtime configuration: limits and multi KV store.

Options for runtime configuration reload can also be configured via YAML:

```yaml
# Configuration file to periodically check and reload.
[file: <string>: default = empty]

# How often to check the file.
[period: <duration>: default 10 seconds]
```

Example runtime configuration file:

```yaml
overrides:
  tenant1:
    ingestion_rate_mb: 10
    max_streams_per_user: 100000
    max_chunks_per_query: 100000
  tenant2:
    max_streams_per_user: 1000000
    max_chunks_per_query: 1000000

multi_kv_config:
    mirror-enabled: false
    primary: consul
```
