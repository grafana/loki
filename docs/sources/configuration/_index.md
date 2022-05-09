---
title: Configuration
weight: 500
---
# Configuring Grafana Loki

Grafana Loki is configured in a YAML file (usually referred to as `loki.yaml` )
which contains information on the Loki server and its individual components,
depending on which mode Loki is launched in.

Configuration examples can be found in the [Configuration Examples](examples/) document.

## Printing Loki Config At Runtime

If you pass Loki the flag `-print-config-stderr` or `-log-config-reverse-order`, (or `-print-config-stderr=true`)
Loki will dump the entire config object it has created from the built in defaults combined first with
overrides from config file, and second by overrides from flags.

The result is the value for every config object in the Loki config struct, which is very large...

Many values will not be relevant to your install such as storage configs which you are not using and which you did not define,
this is expected as every option has a default value if it is being used or not.

This config is what Loki will use to run, it can be invaluable for debugging issues related to configuration and
is especially useful in making sure your config files and flags are being read and loaded properly.

`-print-config-stderr` is nice when running Loki directly e.g. `./loki ` as you can get a quick output of the entire Loki config.

`-log-config-reverse-order` is the flag we run Loki with in all our environments, the config entries are reversed so
that the order of configs reads correctly top to bottom when viewed in Grafana's Explore.

## Configuration File Reference

To specify which configuration file to load, pass the `-config.file` flag at the
command line. The file is written in [YAML format](https://en.wikipedia.org/wiki/YAML),
defined by the scheme below. Brackets indicate that a parameter is optional. For
non-list parameters the value is set to the specified default.

### Use environment variables in the configuration

> **Note:** This feature is only available in Loki 2.1+.

You can use environment variable references in the configuration file to set values that need to be configurable during deployment.
To do this, pass `-config.expand-env=true` and use:

```
${VAR}
```

Where VAR is the name of the environment variable.

Each variable reference is replaced at startup by the value of the environment variable.
The replacement is case-sensitive and occurs before the YAML file is parsed.
References to undefined variables are replaced by empty strings unless you specify a default value or custom error text.

To specify a default value, use:

```
${VAR:-default_value}
```

Where default_value is the value to use if the environment variable is undefined.

Pass the `-config.expand-env` flag at the command line to enable this way of setting configs.

### Generic placeholders

- `<boolean>` : a boolean that can take the values `true` or `false`
- `<int>` : any integer matching the regular expression `[1-9]+[0-9]*`
- `<duration>` : a duration matching the regular expression `[0-9]+(ns|us|µs|ms|[smh])`
- `<labelname>` : a string matching the regular expression `[a-zA-Z_][a-zA-Z0-9_]*`
- `<labelvalue>` : a string of unicode characters
- `<filename>` : a valid path relative to current working directory or an absolute path.
- `<host>` : a valid string consisting of a hostname or IP followed by an optional port number
- `<string>` : a string
- `<secret>` : a string that represents a secret, such as a password

### Supported contents and default values of `loki.yaml`

```yaml
# A comma-separated list of components to run.
# The default value "all" runs Loki in single binary mode.
# The value "read" is an alias to run only read-path related components such as
# the querier and query-frontend, but all in the same process.
# The value "write" is an alias to run only write-path related components such as
# the distributor and compactor, but all in the same process.
# Supported values: all, compactor, distributor, ingester, querier, query-scheduler,
#  ingester-querier, query-frontend, index-gateway, ruler, table-manager, read, write.
# A full list of available targets can be printed when running Loki with the `-list-targets` command line flag.
[target: <string> | default = "all"]

# Enables authentication through the X-Scope-OrgID header, which must be present
# if true. If false, the OrgID will always be set to "fake".
[auth_enabled: <boolean> | default = true]

# The amount of virtual memory in bytes to reserve as ballast in order to optimize
# garbage collection. Larger ballasts result in fewer garbage collection passes, reducing CPU overhead at
# the cost of heap size. The ballast will not consume physical memory, because it is never read from.
# It will, however, distort metrics, because it is counted as live memory.
[ballast_bytes: <int> | default = 0]

# Configures the server of the launched module(s).
[server: <server>]

# Configures the distributor.
[distributor: <distributor>]

# Configures the querier. Only appropriate when running all modules or
# just the querier.
[querier: <querier>]

# The query_scheduler block configures the Loki query scheduler.
# When configured it separates the tenant query queues from the query-frontend
[query_scheduler: <query_scheduler>]

# The frontend block configures the Loki query-frontend.
[frontend: <frontend>]

# The query_range block configures the query splitting and caching in the Loki
# query-frontend.
[query_range: <query_range>]

# The ruler block configures the Loki ruler.
[ruler: <ruler>]

# The ingester_client block configures how the distributor will connect
# to ingesters. Only appropriate when running all components, the distributor,
# or the querier.
[ingester_client: <ingester_client>]

# The ingester block configures the ingester and how the ingester will register itself to a
# key value store.
[ingester: <ingester>]

# Configures where Loki will store data.
[storage_config: <storage_config>]

# Configures how Loki will store data in the specific store.
[chunk_store_config: <chunk_store_config>]

# Configures the chunk index schema and where it is stored.
[schema_config: <schema_config>]

# The compactor block configures the compactor component which compacts index shards for performance.
[compactor: <compactor>]

# Configures limits per-tenant or globally.
[limits_config: <limits_config>]

# The frontend_worker configures the worker - running within the Loki
# querier - picking up and executing queries enqueued by the query-frontend.
[frontend_worker: <frontend_worker>]

# The table_manager block configures the table manager for retention.
[table_manager: <table_manager>]

# Configuration for "runtime config" module, responsible for reloading runtime configuration file.
[runtime_config: <runtime_config>]

# Configuration for tracing.
[tracing: <tracing>]

# Common configuration to be shared between multiple modules.
# If a more specific configuration is given in other sections,
# the related configuration within this section will be ignored.
[common: <common>]

# Configuration for usage report
[analytics: <analytics>]
```

## server

The `server` block
configures the HTTP and gRPC server communication of the launched service(s).

```yaml
# HTTP server listen host
# CLI flag: -server.http-listen-address
[http_listen_address: <string>]

# HTTP server listen port
# CLI flag: -server.http-listen-port
[http_listen_port: <int> | default = 80]

# gRPC server listen host
# CLI flag: -server.grpc-listen-address
[grpc_listen_address: <string>]

# gRPC server listen port
# CLI flag: -server.grpc-listen-port
[grpc_listen_port: <int> | default = 9095]

# Register instrumentation handlers (/metrics, etc.)
# CLI flag: -server.register-instrumentation
[register_instrumentation: <boolean> | default = true]

# Timeout for graceful shutdowns
# CLI flag: -server.graceful-shutdown-timeout
[graceful_shutdown_timeout: <duration> | default = 30s]

# Read timeout for HTTP server
# CLI flag: -server.http-read-timeout
[http_server_read_timeout: <duration> | default = 30s]

# Write timeout for HTTP server
# CLI flag: -server.http-write-timeout
[http_server_write_timeout: <duration> | default = 30s]

# Idle timeout for HTTP server
# CLI flag: -server.http-idle-timeout
[http_server_idle_timeout: <duration> | default = 120s]

# Max gRPC message size that can be received
# CLI flag: -server.grpc-max-recv-msg-size-bytes
[grpc_server_max_recv_msg_size: <int> | default = 4194304]

# Max gRPC message size that can be sent
# CLI flag: -server.grpc-max-send-msg-size-bytes
[grpc_server_max_send_msg_size: <int> | default = 4194304]

# Limit on the number of concurrent streams for gRPC calls (0 = unlimited)
# CLI flag: -server.grpc-max-concurrent-streams
[grpc_server_max_concurrent_streams: <int> | default = 100]

# Log only messages with the given severity or above. Supported values [debug,
# info, warn, error]
# CLI flag: -log.level
[log_level: <string> | default = "info"]

# Base path to serve all API routes from (e.g., /v1/).
# CLI flag: -server.path-prefix
[http_path_prefix: <string> | default = ""]
```

## distributor

The `distributor` block configures the distributor component.

```yaml
# Configures the distributors ring, used when the "global" ingestion rate
# strategy is enabled.
ring:
  kvstore:
    # The backend storage to use for the ring. Supported values are
    # consul, etcd, inmemory, memberlist
    # CLI flag: -distributor.ring.store
    store: <string>

    # The prefix for the keys in the store. Should end with a /.
    # CLI flag: -distributor.ring.prefix
    [prefix: <string> | default = "collectors/"]

    # Configuration for a Consul client. Only applies if store is "consul"
    # The CLI flags prefix for this block config is: distributor.ring
    [consul: <consul_config>]

    # Configuration for an ETCD v3 client. Only applies if store is "etcd"
    # The CLI flags prefix for this block config is: distributor.ring
    [etcd: <etcd_config>]

  # The heartbeat timeout after which ingesters are skipped for
  # reading and writing.
  # CLI flag: -distributor.ring.heartbeat-timeout
  [heartbeat_timeout: <duration> | default = 1m]
```

## querier

The `querier` block configures the Loki Querier.

```yaml
# Timeout when querying ingesters or storage during the execution of a query request.
# CLI flag: -querier.query-timeout
[query_timeout: <duration> | default = 1m]

# Maximum duration for which the live tailing requests should be served.
# CLI flag: -querier.tail-max-duration
[tail_max_duration: <duration> | default = 1h]

# Time to wait before sending more than the minimum successful query requests.
# CLI flag: -querier.extra-query-delay
[extra_query_delay: <duration> | default = 0s]

# Maximum lookback beyond which queries are not sent to ingester.
# 0 means all queries are sent to ingester.
# CLI flag: -querier.query-ingesters-within
[query_ingesters_within: <duration> | default = 3h]

# The maximum number of concurrent queries allowed.
# CLI flag: -querier.max-concurrent
[max_concurrent: <int> | default = 10]

# Only query the store, do not attempt to query any ingesters,
# useful for running a standalone querier pool opearting only against stored data.
# CLI flag: -querier.query-store-only
[query_store_only: <boolean> | default = false]

# Queriers should only query the ingesters and not try to query any store,
# useful for when object store is unavailable.
# CLI flag: -querier.query-ingester-only
[query_ingester_only: <boolean> | default = false]

# Allow queries for multiple tenants.
# CLI flag: -querier.multi-tenant-queries-enabled
[multi_tenant_queries_enabled: <boolean> | default = false]

# Configuration options for the LogQL engine.
engine:
  # Timeout for query execution
  # CLI flag: -querier.engine.timeout
  [timeout: <duration> | default = 3m]

  # The maximum amount of time to look back for log lines. Only
  # applicable for instant log queries.
  # CLI flag: -querier.engine.max-lookback-period
  [max_look_back_period: <duration> | default = 30s]
```

## query_scheduler

The `query_scheduler` block configures the Loki query scheduler.

```yaml
# Maximum number of outstanding requests per tenant per query-scheduler.
# In-flight requests above this limit will fail with HTTP response status code
# 429.
# CLI flag: -query-scheduler.max-outstanding-requests-per-tenant
[max_outstanding_requests_per_tenant: <int> | default = 100]

# This configures the gRPC client used to report errors back to the
# query-frontend.
[grpc_client_config: <grpc_client_config>]

# Set to true to have the query schedulers create and place themselves in a ring.
# If no frontend_address or scheduler_address are present
# anywhere else in the configuration, Loki will toggle this value to true.
[use_scheduler_ring: <boolean> | default = false]

# The hash ring configuration. This option is required only if use_scheduler_ring is true
# The CLI flags prefix for this block config is scheduler.ring
[scheduler_ring: <ring>]
```

## frontend

The `frontend` block configures the Loki query-frontend.

```yaml
# Maximum number of outstanding requests per tenant per frontend; requests
# beyond this error with HTTP 429.
# CLI flag: -querier.max-outstanding-requests-per-tenant
[max_outstanding_per_tenant: <int> | default = 2048]

# In the event a tenant is repeatedly sending queries that lead the querier to crash
# or be killed due to an out-of-memory error, the crashed querier will be disconnected
# from the query frontend and a new querier will be immediately assigned to the tenant’s shard.
# This invalidates the assumption that shuffle sharding can be used to reduce the
# impact on tenants. This option mitigates the impact by configuring a delay between when
# a querier disconnects because of a crash and when the crashed querier is actually removed
# from the tenant's shard.
# CLI flag: -query-frontend.querier-forget-delay
[querier_forget_delay: <duration> | default = 0s]

# Compress HTTP responses.
# CLI flag: -querier.compress-http-responses
[compress_responses: <boolean> | default = false]

# URL of downstream Loki.
# CLI flag: -frontend.downstream-url
[downstream_url: <string> | default = ""]

# Address, including port, where the compactor api is served
# CLI flag: -frontend.compactor-address
[compactor_address: <string> | default = ""]

# Log queries that are slower than the specified duration. Set to 0 to disable.
# Set to < 0 to enable on all queries.
# CLI flag: -frontend.log-queries-longer-than
[log_queries_longer_than: <duration> | default = 0s]

# URL of querier for tail proxy.
# CLI flag: -frontend.tail-proxy-url
[tail_proxy_url: <string> | default = ""]

# DNS hostname used for finding query-schedulers.
# CLI flag: -frontend.scheduler-address
[scheduler_address: <string> | default = ""]

# How often to resolve the scheduler-address, in order to look for new
# query-scheduler instances.
# Also used to determine how often to poll the scheduler-ring for addresses if configured.
# CLI flag: -frontend.scheduler-dns-lookup-period
[scheduler_dns_lookup_period: <duration> | default = 10s]

# Number of concurrent workers forwarding queries to single query-scheduler.
# CLI flag: -frontend.scheduler-worker-concurrency
[scheduler_worker_concurrency: <int> | default = 5]
```

## query_range

The `query_range` block configures query splitting and caching in the Loki query-frontend.

```yaml
# Deprecated: Split queries by day and execute in parallel.
# Use -querier.split-queries-by-interval instead.
# CLI flag: -querier.split-queries-by-day
[split_queries_by_day: <boolean> | default = false]

# Mutate incoming queries to align their start and end with their step.
# CLI flag: -querier.align-querier-with-step
[align_queries_with_step: <boolean> | default = false]

results_cache:
  # The CLI flags prefix for this block config is: frontend
  cache: <cache_config>

# Cache query results.
# CLI flag: -querier.cache-results
[cache_results: <boolean> | default = false]

# Maximum number of retries for a single request; beyond this, the downstream
# error is returned.
# CLI flag: -querier.max-retries-per-request
[max_retries: <int> | default = 5]

# Perform query parallelisations based on storage sharding configuration and
# query ASTs. This feature is supported only by the chunks storage engine.
# CLI flag: -querier.parallelise-shardable-queries
[parallelise_shardable_queries: <boolean> | default = true]
```

## ruler

The `ruler` block configures the Loki ruler.

```yaml
# URL of alerts return path.
# CLI flag: -ruler.external.url
[external_url: <url> | default = ]

# Labels to add to all alerts
external_labels:
  [<labelname>: <labelvalue> ...]

ruler_client:
  # Path to the client certificate file, which will be used for authenticating
  # with the server. Also requires the key path to be configured.
  # CLI flag: -ruler.client.tls-cert-path
  [tls_cert_path: <string> | default = ""]

  # Path to the key file for the client certificate. Also requires the client
  # certificate to be configured.
  # CLI flag: -ruler.client.tls-key-path
  [tls_key_path: <string> | default = ""]

  # Path to the CA certificates file to validate server certificate against. If
  # not set, the host's root CA certificates are used.
  # CLI flag: -ruler.client.tls-ca-path
  [tls_ca_path: <string> | default = ""]

  # Skip validating server certificate.
  # CLI flag: -ruler.client.tls-insecure-skip-verify
  [tls_insecure_skip_verify: <boolean> | default = false]

# How frequently to evaluate rules.
# CLI flag: -ruler.evaluation-interval
[evaluation_interval: <duration> | default = 1m]

# How frequently to poll for rule changes.
# CLI flag: -ruler.poll-interval
[poll_interval: <duration> | default = 1m]

storage:
  # Method to use for backend rule storage (azure, gcs, s3, swift, local, bos).
  # CLI flag: -ruler.storage.type
  [type: <string> ]

  # Configures backend rule storage for Azure.
  [azure: <azure_storage_config>]

  # Configures backend rule storage for GCS.
  [gcs: <gcs_storage_config>]

  # Configures backend rule storage for S3.
  [s3: <s3_storage_config>]

  # Configures backend rule storage for Swift.
  [swift: <swift_storage_config>]

  # Configures backend rule storage for a local file system directory.
  [local: <local_storage_config>]

  # Configures backend rule storage for Baidu Object Storage (BOS).
  [bos: <bos_storage_config>]

  # The `hedging` block configures how to hedge storage requests.
  [hedging: <hedging>]

# Remote-write configuration to send rule samples to a Prometheus remote-write endpoint.
remote_write:
  # Enable remote-write functionality.
  # CLI flag: -ruler.remote-write.enabled
  [enabled: <boolean> | default = false]
  # Minimum period to wait between refreshing remote-write reconfigurations.
  # This should be greater than or equivalent to -limits.per-user-override-period.
  [config_refresh_period: <duration> | default = 10s]

  client:
    # The URL of the endpoint to send samples to.
    url: <string>

    # Timeout for requests to the remote write endpoint.
    [remote_timeout: <duration> | default = 30s]

    # Custom HTTP headers to be sent along with each remote write request.
    # Be aware that headers that are set by Loki itself can't be overwritten.
    headers:
      [<string>: <string> ...]

    # List of remote write relabel configurations.
    write_relabel_configs:
      [- <relabel_config> ...]

    # Name of the remote write config, which if specified must be unique among remote
    # write configs.
    # The name will be used in metrics and logging in place of a generated value
    # to help users distinguish between remote write configs.
    [name: <string>]

    # Sets the `Authorization` header on every remote write request with the
    # configured username and password.
    # password and password_file are mutually exclusive.
    basic_auth:
      [username: <string>]
      [password: <secret>]
      [password_file: <string>]

    # Optional `Authorization` header configuration.
    authorization:
      # Sets the authentication type.
      [type: <string> | default: Bearer]
      # Sets the credentials. It is mutually exclusive with
      # `credentials_file`.
      [credentials: <secret>]
      # Sets the credentials to the credentials read from the configured file.
      # It is mutually exclusive with `credentials`.
      [credentials_file: <filename>]

    # Optionally configures AWS's Signature Verification 4 signing process to
    # sign requests. Cannot be set at the same time as basic_auth, authorization, or oauth2.
    # To use the default credentials from the AWS SDK, use `sigv4: {}`.
    sigv4:
      # The AWS region. If blank, the region from the default credentials chain
      # is used.
      [region: <string>]

      # The AWS API keys. If blank, the environment variables `AWS_ACCESS_KEY_ID`
      # and `AWS_SECRET_ACCESS_KEY` are used.
      [access_key: <string>]
      [secret_key: <secret>]

      # Named AWS profile used to authenticate.
      [profile: <string>]

      # AWS Role ARN, an alternative to using AWS API keys.
      [role_arn: <string>]

    # Configures the remote write request's TLS settings.
    tls_config:
      # CA certificate to validate API server certificate with.
      [ca_file: <filename>]
      # Certificate and key files for client cert authentication to the server.
      [cert_file: <filename>]
      [key_file: <filename>]
      # ServerName extension to indicate the name of the server.
      # https://tools.ietf.org/html/rfc4366#section-3.1
      [server_name: <string>]
      # Disable validation of the server certificate.
      [insecure_skip_verify: <boolean>]

    # Optional proxy URL.
    [proxy_url: <string>]

    # Configure whether HTTP requests follow HTTP 3xx redirects.
    [follow_redirects: <boolean> | default = true]

    # Configures the queue used to write to remote storage.
    queue_config:
      # Number of samples to buffer per shard before we block reading of more
      # samples from the WAL. It is recommended to have enough capacity in each
      # shard to buffer several requests to keep throughput up while processing
      # occasional slow remote requests.
      [capacity: <int> | default = 2500]
      # Maximum number of shards, i.e. amount of concurrency.
      [max_shards: <int> | default = 200]
      # Minimum number of shards, i.e. amount of concurrency.
      [min_shards: <int> | default = 1]
      # Maximum number of samples per send.
      [max_samples_per_send: <int> | default = 500]
      # Maximum time a sample will wait in buffer.
      [batch_send_deadline: <duration> | default = 5s]
      # Initial retry delay. Gets doubled for every retry.
      [min_backoff: <duration> | default = 30ms]
      # Maximum retry delay.
      [max_backoff: <duration> | default = 100ms]
      # Retry upon receiving a 429 status code from the remote-write storage.
      # This is experimental and might change in the future.
      [retry_on_http_429: <boolean> | default = false]

wal:
  # The directory in which to write tenant WAL files. Each tenant will have its own
  # directory one level below this directory.
  [dir: <string> | default = "ruler-wal"]
  # Frequency with which to run the WAL truncation process.
  [truncate_frequency: <duration> | default = 60m]
  # Minimum and maximum time series should exist in the WAL for.
  [min_age: <duration> | default = 5m]
  [max_age: <duration> | default = 4h]

wal_cleaner:
  # The minimum age of a WAL to consider for cleaning.
  [min_age: <duration> | default = 12h]
  # How often to run the WAL cleaner.
  [period: <duration> | default = 0s (disabled)]

# File path to store temporary rule files.
# CLI flag: -ruler.rule-path
[rule_path: <filename> | default = "/rules"]

# Comma-separated list of Alertmanager URLs to send notifications to.
# Each Alertmanager URL is treated as a separate group in the configuration.
# Multiple Alertmanagers in HA per group can be supported by using DNS
# resolution via -ruler.alertmanager-discovery.
# CLI flag: -ruler.alertmanager-url
[alertmanager_url: <string> | default = ""]

# Use DNS SRV records to discover Alertmanager hosts.
# CLI flag: -ruler.alertmanager-discovery
[enable_alertmanager_discovery: <boolean> | default = false]

# How long to wait between refreshing DNS resolutions of Alertmanager hosts.
# CLI flag: -ruler.alertmanager-refresh-interval
[alertmanager_refresh_interval: <duration> | default = 1m]

# If enabled, then requests to Alertmanager use the v2 API.
# CLI flag: -ruler.alertmanager-use-v2
[enable_alertmanager_v2: <boolean> | default = false]

# Capacity of the queue for notifications to be sent to the Alertmanager.
# CLI flag: -ruler.notification-queue-capacity
[notification_queue_capacity: <int> | default = 10000]

# HTTP timeout duration when sending notifications to the Alertmanager.
# CLI flag: -ruler.notification-timeout
[notification_timeout: <duration> | default = 10s]

# Max time to tolerate outage for restoring "for" state of alert.
# CLI flag: -ruler.for-outage-tolerance
[for_outage_tolerance: <duration> | default = 1h]

# Minimum duration between alert and restored "for" state. This is maintained
# only for alerts with configured "for" time greater than the grace period.
# CLI flag: -ruler.for-grace-period
[for_grace_period: <duration> | default = 10m]

# Minimum amount of time to wait before resending an alert to Alertmanager.
# CLI flag: -ruler.resend-delay
[resend_delay: <duration> | default = 1m]

# Distribute rule evaluation using ring backend.
# CLI flag: -ruler.enable-sharding
[enable_sharding: <boolean> | default = false]

# Time to spend searching for a pending ruler when shutting down.
# CLI flag: -ruler.search-pending-for
[search_pending_for: <duration> | default = 5m]

# Ring used by Loki ruler.
# The CLI flags prefix for this block config is ruler.ring
ring:
  kvstore:
    # Backend storage to use for the ring. Supported values are: consul, etcd,
    # inmemory, memberlist, multi.
    # CLI flag: -<prefix>.store
    [store: <string> | default = "memberlist"]

    # The prefix for the keys in the store. Should end with a /.
    # CLI flag: -<prefix>.prefix
    [prefix: <string> | default = "collectors/"]

    # The consul_config configures the consul client.
    [consul: <consul_config>]

    # The etcd_config configures the etcd client.
    [etcd: <etcd_config>]

    multi:
      # Primary backend storage used by multi-client.
      # CLI flag: -<prefix>.multi.primary
      [primary: <string> | default = ""]

      # Secondary backend storage used by multi-client.
      # CLI flag: -<prefix>.multi.secondary
      [secondary: <string> | default = ""]

      # Mirror writes to secondary store.
      # CLI flag: -<prefix>.multi.mirror-enabled
      [mirror_enabled: <boolean> | default = false]

      # Timeout for storing value to secondary store.
      # CLI flag: -<prefix>.multi.mirror-timeout
      [mirror_timeout: <duration> | default = 2s]

  # Interval between heartbeats sent to the ring. 0 = disabled.
  # CLI flag: -<prefix>.heartbeat-period
  [heartbeat_period: <duration> | default = 15s]

  # The heartbeat timeout after which ruler ring members are considered unhealthy
  # within the ring. 0 = never (timeout disabled).
  # CLI flag: -<prefix>.heartbeat-timeout
  [heartbeat_timeout: <duration> | default = 1m]

  # Name of network interface to read addresses from.
  # CLI flag: -<prefix>.instance-interface-names
  [instance_interface_names: <list of string> | default = [<private network interfaces>]]

  # The number of tokens the lifecycler will generate and put into the ring if
  # it joined without transferring tokens from another lifecycler.
  # CLI flag: -<prefix>.num-tokens
  [num_tokens: <int> | default = 128]
```

## bos_storage_config

The `bos_storage_config` block configures Baidu Object Storage (BOS)  as general storage for data generated by Loki.

```yaml
# Name of BOS bucket.
# CLI flag: <prefix>.baidubce.bucket-name
[ bucket_name: <string> | default = "" ]
# BOS endpoint to connect to.
# CLI flag: <prefix>.baidubce.endpoint
[ endpoint: <string> | default = "bj.bcebos.com" ]
# Baidu Cloud Engine (BCE) Access Key ID
# CLI flag: <prefix>.baidubce.access-key-id
[ access_key_id: <string> | default = "" ]
# BCE Secret Access Key
# CLI flag: <prefix>.baidubce.secret-access-key
[ secret_access_key: <string> | default = "" ]
```

## azure_storage_config

The `azure_storage_config` configures Azure as a general storage for different data generated by Loki.

```yaml
# Azure Cloud environment. Supported values are: AzureGlobal,
# AzureChinaCloud, AzureGermanCloud, AzureUSGovernment.
# CLI flag: -<prefix>.azure.environment
[environment: <string> | default = "AzureGlobal"]

# Name of the blob container used to store chunks. This container must be
# created before running cortex.
# CLI flag: -<prefix>.azure.container-name
[container_name: <string> | default = "loki"]

# The Microsoft Azure account name to be used
# CLI flag: -<prefix>.azure.account-name
[account_name: <string> | default = ""]

# The Microsoft Azure account key to use.
# CLI flag: -<prefix>.azure.account-key
[account_key: <string> | default = ""]

# Chunk delimiter to build the blobID
# CLI flag: -<prefix>.azure.chunk-delimiter
[chunk_delimiter: <string> | default = "-"]

# Preallocated buffer size for downloads.
# CLI flag: -<prefix>.azure.download-buffer-size
[download_buffer_size: <int> | default = 512000]

# Preallocated buffer size for uploads.
# CLI flag: -<prefix>.azure.upload-buffer-size
[upload_buffer_size: <int> | default = 256000]

# Number of buffers used to used to upload a chunk.
# CLI flag: -<prefix>.azure.download-buffer-count
[upload_buffer_count: <int> | default = 1]

# Timeout for requests made against azure blob storage.
# CLI flag: -<prefix>.azure.request-timeout
[request_timeout: <duration> | default = 30s]

# Number of retries for a request which times out.
# CLI flag: -<prefix>.azure.max-retries
[max_retries: <int> | default = 5]

# Minimum time to wait before retrying a request.
# CLI flag: -<prefix>.azure.min-retry-delay
[min_retry_delay: <duration> | default = 10ms]

# Maximum time to wait before retrying a request.
# CLI flag: -<prefix>.azure.max-retry-delay
[max_retry_delay: <duration> | default = 500ms]

# Use Managed Identity or not.
# CLI flag: -ruler.storage.azure.use-managed-identity
[use_managed_identity: <boolean> | default = false]
```

## gcs_storage_config

The `gcs_storage_config` configures GCS as a general storage for different data generated by Loki.

```yaml
# Name of GCS bucket to put chunks in.
# CLI flag: -<prefix>.gcs.bucketname
[bucket_name: <string> | default = ""]

# The size of the buffer that GCS client for each PUT request. 0 to disable
# buffering.
# CLI flag: -<prefix>.gcs.chunk-buffer-size
[chunk_buffer_size: <int> | default = 0]

# The duration after which the requests to GCS should be timed out.
# CLI flag: -<prefix>.gcs.request-timeout
[request_timeout: <duration> | default = 0s]

# Enable HTTP/2 when connecting to GCS. This configuration only applies to GET operations.
# CLI flag: -<prefix>.gcs.enable-http2
[enable_http2: <boolean> | default = true]
```

## s3_storage_config

The `s3_storage_config` configures S3 as a general storage for different data generated by Loki.

```yaml
# S3 endpoint URL with escaped Key and Secret encoded. If only region is
# specified as a host, proper endpoint will be deduced. Use
# inmemory:///<bucket-name> to use a mock in-memory implementation.
# CLI flag: -<prefix>.s3.url
[s3: <url> | default = ]

# Set this to `true` to force the request to use path-style addressing.
# CLI flag: -<prefix>.s3.force-path-style
[s3forcepathstyle: <boolean> | default = false]

# Comma separated list of bucket names to evenly distribute chunks over.
# Overrides any buckets specified in s3.url flag
# CLI flag: -<prefix>.s3.buckets
[bucketnames: <string> | default = ""]

# S3 Endpoint to connect to.
# CLI flag: -<prefix>.s3.endpoint
[endpoint: <string> | default = ""]

# AWS region to use.
# CLI flag: -<prefix>.s3.region
[region: <string> | default = ""]

# AWS Access Key ID
# CLI flag: -<prefix>.s3.access-key-id
[access_key_id: <string> | default = ""]

# AWS Secret Access Key
# CLI flag: -<prefix>.s3.secret-access-key
[secret_access_key: <string> | default = ""]

# Disable https on S3 connection.
# CLI flag: -<prefix>.s3.insecure
[insecure: <boolean> | default = false]

# Enable AES256 AWS server-side encryption
# CLI flag: -<prefix>.s3.sse-encryption
[sse_encryption: <boolean> | default = false]

http_config:
  # The maximum amount of time an idle connection will be held open.
  # CLI flag: -<prefix>.s3.http.idle-conn-timeout
  [idle_conn_timeout: <duration> | default = 1m30s]

  # If non-zero, specifies the amount of time to wait for a server's
  # response headers after fully writing the request.
  # CLI flag: -<prefix>.s3.http.response-header-timeout
  [response_header_timeout: <duration> | default = 0s]

  # Set to true to skip verifying the certificate chain and hostname.
  # CLI flag: -<prefix>.s3.http.insecure-skip-verify
  [insecure_skip_verify: <boolean> | default = false]

  # Path to the trusted CA file that signed the SSL certificate of the S3
  # endpoint.
  # CLI flag: -<prefix>.s3.http.ca-file
  [ca_file: <string> | default = ""]
```

## swift_storage_config

The `swift_storage_config` configures Swift as a general storage for different data generated by Loki.

```yaml
# Openstack authentication URL.
# CLI flag: -<prefix>.swift.auth-url
[auth_url: <string> | default = ""]

# Openstack username for the api.
# CLI flag: -<prefix>.swift.username
[username: <string> | default = ""]

# Openstack user's domain name.
# CLI flag: -<prefix>.swift.user-domain-name
[user_domain_name: <string> | default = ""]

# Openstack user's domain ID.
# CLI flag: -<prefix>.swift.user-domain-id
[user_domain_id: <string> | default = ""]

# Openstack user ID for the API.
# CLI flag: -<prefix>.swift.user-id
[user_id: <string> | default = ""]

# Openstack API key.
# CLI flag: -<prefix>.swift.password
[password: <string> | default = ""]

# Openstack user's domain ID.
# CLI flag: -<prefix>.swift.domain-id
[domain_id: <string> | default = ""]

# Openstack user's domain name.
# CLI flag: -<prefix>.swift.domain-name
[domain_name: <string> | default = ""]

# Openstack project ID (v2,v3 auth only).
# CLI flag: -<prefix>.swift.project-id
[project_id: <string> | default = ""]

# Openstack project name (v2,v3 auth only).
# CLI flag: -<prefix>.swift.project-name
[project_name: <string> | default = ""]

# ID of the project's domain (v3 auth only), only needed if it differs the
# from user domain.
# CLI flag: -<prefix>.swift.project-domain-id
[project_domain_id: <string> | default = ""]

# Name of the project's domain (v3 auth only), only needed if it differs
# from the user domain.
# CLI flag: -<prefix>.swift.project-domain-name
[project_domain_name: <string> | default = ""]

# Openstack Region to use eg LON, ORD - default is use first region (v2,v3
# auth only)
# CLI flag: -<prefix>.swift.region-name
[region_name: <string> | default = ""]

# Name of the Swift container to put chunks in.
# CLI flag: -<prefix>.swift.container-name
[container_name: <string> | default = ""]
```

## hedging

The `hedging` block configures how to hedge storage requests.

The hedging implementation sends a second storage request once a first request has
been outstanding for more than a configured expected latency for this class of requests.
Calculate your latency to be the 99th percentile of object storage response times.

```yaml
# An optional duration that sets the quantity of time after a first storage request
# is sent and before a second request is sent, when no response is received for the first
# storage request. The recommended duration is the measured 99th percentile of object
# storage response times, to reduce long tail latency. This option is most impactful
# when used with queriers, and has minimal to no impact on other components.
# The default value of 0 disables the hedging of storage requests.
# Example: "at: 500ms"
[at: <duration> | default = 0]

# An optional maximum quantity of hedged requests to be issued for a given request.
[up_to: <int> | default = 2]

# Caps the rate of hedged requests by optionally defining the maximum quantity of
# hedged requests issued per second.
[max_per_second: <int> | default = 5]
```

## local_storage_config

The `local_storage_config` configures a (local) file system as a general storage for different data generated by Loki.

```yaml
# Filesystem directory to be used as storage.
# CLI flag: -<prefix>.local.directory
[directory: <filename> | default = ""]
```

## frontend_worker

The `frontend_worker` configures the worker - running within the Loki querier - picking up and executing queries enqueued by the query-frontend.

```yaml
# Address of query frontend service, in host:port format.
# CLI flag: -querier.frontend-address
[frontend_address: <string> | default = ""]

# Number of simultaneous queries to process.
# CLI flag: -querier.worker-parallelism
[parallelism: <int> | default = 10]

# Force worker concurrency to match the -querier.max-concurrent option. Overrides querier.worker-parallelism.
# CLI flag: -querier.worker-match-max-concurrent
[match_max_concurrent: <boolean> | default = true]

# How often to query the frontend_address DNS to resolve frontend addresses.
# Also used to determine how often to poll the scheduler-ring for addresses if configured.
# CLI flag: -querier.dns-lookup-period
[dns_lookup_duration: <duration> | default = 3s]

# The CLI flags prefix for this block config is: querier.frontend-client
[grpc_client_config: <grpc_client_config>]

# DNS hostname used for finding query-schedulers.
# CLI flag: -querier.scheduler-address
[scheduler_address: <string> | default = ""]
```

## ingester_client

The `ingester_client` block configures how connections to ingesters
operate.

```yaml
# Configures how connections are pooled
pool_config:
  # Whether or not to do health checks.
  # CLI flag: -distributor.health-check-ingesters
  [health_check_ingesters: <boolean> | default = false]

  # How frequently to clean up clients for servers that have gone away after
  # a health check.
  # CLI flag: -distributor.client-cleanup-period
  [client_cleanup_period: <duration> | default = 15s]

  # How quickly a dead client will be removed after it has been detected
  # to disappear. Set this to a value to allow time for a secondary
  # health check to recover the missing client.
  # CLI flag: -ingester.client.healthcheck-timeout
  [remote_timeout: <duration> | default = 1s]

# The remote request timeout on the client side.
# CLI flag: -ingester.client.timeout
[remote_timeout: <duration> | default = 5s]

# Configures how the gRPC connection to ingesters work as a client
# The CLI flags prefix for this block config is: ingester.client
[grpc_client_config: <grpc_client_config>]
```

## ingester

The `ingester` block configures the Loki Ingesters.

```yaml
# Configures how the lifecycle of the ingester will operate
# and where it will register for discovery.
lifecycler:
  ring:
    kvstore:
      # Backend storage to use for the ring. Supported values are: consul, etcd,
      # inmemory, memberlist
      # CLI flag: -ring.store
      [store: <string> | default = "consul"]

      # The prefix for the keys in the store. Should end with a /.
      # CLI flag: -ring.prefix
      [prefix: <string> | default = "collectors/"]

      # The consul_config configures the consul client.
      # CLI flag: <no prefix>
      [consul: <consul_config>]

      # The etcd_config configures the etcd client.
      # CLI flag: <no prefix>
      [etcd: <etcd_config>]

    # The heartbeat timeout after which ingesters are skipped for reads/writes.
    # CLI flag: -ring.heartbeat-timeout
    [heartbeat_timeout: <duration> | default = 1m]

    # The number of ingesters to write to and read from.
    # CLI flag: -distributor.replication-factor
    [replication_factor: <int> | default = 3]

  # The number of tokens the lifecycler will generate and put into the ring if
  # it joined without transferring tokens from another lifecycler.
  # CLI flag: -ingester.num-tokens
  [num_tokens: <int> | default = 128]

  # Period at which to heartbeat to the underlying ring.
  # CLI flag: -ingester.heartbeat-period
  [heartbeat_period: <duration> | default = 5s]

  # How long to wait to claim tokens and chunks from another member when
  # that member is leaving. Will join automatically after the duration expires.
  # CLI flag: -ingester.join-after
  [join_after: <duration> | default = 0s]

  # Observe tokens after generating to resolve collisions. Useful when using a gossip ring.
  # CLI flag: -ingester.observe-period
  [observe_period: <duration> | default = 0s]

  # Minimum duration to wait before becoming ready. This is to work around race
  # conditions with ingesters exiting and updating the ring.
  # CLI flag: -ingester.min-ready-duration
  [min_ready_duration: <duration> | default = 15s]

  # Name of network interfaces to read addresses from.
  # CLI flag: -ingester.lifecycler.interface
  interface_names:

    - [<string> ... | default = [<private network interfaces>]]

  # Duration to sleep before exiting to ensure metrics are scraped.
  # CLI flag: -ingester.final-sleep
  [final_sleep: <duration> | default = 30s]

# Number of times to try and transfer chunks when leaving before
# falling back to flushing to the store. Zero = no transfers are done.
# CLI flag: -ingester.max-transfer-retries
[max_transfer_retries: <int> | default = 0]

# How many flushes can happen concurrently from each stream.
# CLI flag: -ingester.concurrent-flushes
[concurrent_flushes: <int> | default = 32]

# How often should the ingester see if there are any blocks to flush
# CLI flag: -ingester.flush-check-period
[flush_check_period: <duration> | default = 30s]

# The timeout before a flush is cancelled
# CLI flag: -ingester.flush-op-timeout
[flush_op_timeout: <duration> | default = 10m]

# How long chunks should be retained in-memory after they've been flushed.
# CLI flag: -ingester.chunks-retain-period
[chunk_retain_period: <duration> | default = 0s]

# How long chunks should sit in-memory with no updates before
# being flushed if they don't hit the max block size. This means
# that half-empty chunks will still be flushed after a certain
# period as long as they receive no further activity.
# CLI flag: -ingester.chunks-idle-period
[chunk_idle_period: <duration> | default = 30m]

# The targeted _uncompressed_ size in bytes of a chunk block
# When this threshold is exceeded the head block will be cut and compressed inside the chunk.
# CLI flag: -ingester.chunks-block-size
[chunk_block_size: <int> | default = 262144]

# A target _compressed_ size in bytes for chunks.
# This is a desired size not an exact size, chunks may be slightly bigger
# or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period)
# A value of 0 creates chunks with a fixed 10 blocks,
# A non zero value will create chunks with a variable number of blocks to meet the target size.
# CLI flag: -ingester.chunk-target-size
[chunk_target_size: <int> | default = 1572864]

# The compression algorithm to use for chunks. (supported: gzip, lz4, snappy)
# You should choose your algorithm depending on your need:
# - `gzip` highest compression ratio but also slowest decompression speed. (144 kB per chunk)
# - `lz4` fastest compression speed (188 kB per chunk)
# - `snappy` fast and popular compression algorithm (272 kB per chunk)
# CLI flag: -ingester.chunk-encoding
[chunk_encoding: <string> | default = gzip]

# Parameters used to synchronize ingesters to cut chunks at the same moment.
# Sync period is used to roll over incoming entry to a new chunk. If chunk's utilization
# isn't high enough (eg. less than 50% when sync_min_utilization is set to 0.5), then
# this chunk rollover doesn't happen.
# CLI flag: -ingester.sync-period
[sync_period: <duration> | default = 0]

# CLI flag: -ingester.sync-min-utilization
[sync_min_utilization: <float> | Default = 0]

# The maximum number of errors a stream will report to the user
# when a push fails. 0 to make unlimited.
# CLI flag: -ingester.max-ignored-stream-errors
[max_returned_stream_errors: <int> | default = 10]

# The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this,
# the current chunk will be flushed to the store and a new chunk created.
# CLI flag: -ingester.max-chunk-age
[max_chunk_age: <duration> | default = 2h]

# How far in the past an ingester is allowed to query the store for data.
# This is only useful for running multiple Loki binaries with a shared ring
# with a `filesystem` store, which is NOT shared between the binaries.
# When using any "shared" object store like S3 or GCS, this value must always be left as 0.
# It is an error to configure this to a non-zero value when using any object store other
# than `filesystem`.
# Use a value of -1 to allow the ingester to query the store infinitely far back in time.
# CLI flag: -ingester.query-store-max-look-back-period
[query_store_max_look_back_period: <duration> | default = 0]

# Forget about ingesters having heartbeat timestamps older than `ring.kvstore.heartbeat_timeout`.
# This is equivalent to clicking on the `/ring` `forget` button in the UI:
# the ingester is removed from the ring.
# This is a useful setting when you are sure that an unhealthy node won't return.
# An example is when not using stateful sets or the equivalent.
# Use `memberlist.rejoin_interval` > 0 to handle network partition cases when using a memberlist.
# CLI flag: -ingester.autoforget-unhealthy
[autoforget_unhealthy: <boolean> | default = false]

# The ingester WAL (Write Ahead Log) records incoming logs and stores them on
# the local file systems in order to guarantee persistence of acknowledged data
# in the event of a process crash.
wal:
  # Enables writing to WAL.
  # CLI flag: -ingester.wal-enabled
  [enabled: <boolean> | default = true]

  # Directory where the WAL data should be stored and/or recovered from.
  # CLI flag: -ingester.wal-dir
  [dir: <filename> | default = "wal"]

  # When WAL is enabled, should chunks be flushed to long-term storage on shutdown.
  # CLI flag: -ingester.flush-on-shutdown
  [flush_on_shutdown: <boolean> | default = false]

  # Interval at which checkpoints should be created.
  # CLI flag: ingester.checkpoint-duration
  [checkpoint_duration: <duration> | default = 5m]

  # Maximum memory size the WAL may use during replay. After hitting this,
  # it will flush data to storage before continuing.
  # A unit suffix (KB, MB, GB) may be applied.
  [replay_memory_ceiling: <string> | default = 4GB]

# Shard factor used in the ingesters for the in process reverse index.
# This MUST be evenly divisible by ALL schema shard factors or Loki will not start.
[index_shards: <int> | default = 32]
```

## consul_config

The `consul_config` configures the consul client. The supported CLI flags <prefix> used to reference this config block are:

```yaml
 # The hostname and port of Consul.
# CLI flag: -<prefix>.consul.hostname
[host: <string> | default = "localhost:8500"]

# The ACL Token used to interact with Consul.
# CLI flag: -<prefix>.consul.acl-token
[acl_token: <string>]

# The HTTP timeout when communicating with Consul
# CLI flag: -<prefix>.consul.client-timeout
[http_client_timeout: <duration> | default = 20s]

# Whether or not consistent reads to Consul are enabled.
# CLI flag: -<prefix>.consul.consistent-reads
[consistent_reads: <boolean> | default = true]
```

## etcd_config

The `etcd_config` configures the etcd client. The supported CLI flags <prefix> used to reference this config block are:

```yaml
# The etcd endpoints to connect to.
# CLI flag: -<prefix>.etcd.endpoints
[endpoints: <list of string> | default = []]

# The dial timeout for the etcd connection.
# CLI flag: -<prefix>.etcd.dial-timeout
[dial_timeout: <duration> | default = 10s]

# The maximum number of retries to do for failed ops.
# CLI flag: -<prefix>.etcd.max-retries
[max_retries: <int> | default = 10]
```

## memberlist_config

The `memberlist_config` block configures the gossip ring to discover and connect
between distributors, ingesters and queriers. The configuration is unique for all
three components to ensure a single shared ring.

When a `memberlist_config` with least 1 `join_members` is defined, a `kvstore` of type `memberlist` is
automatically configured for the `distributor`, `ingester`, and `ruler` rings unless otherwise specified in
those components specific configuration sections.

```yaml
# Name of the node in memberlist cluster. Defaults to hostname.
# CLI flag: -memberlist.nodename
[node_name: <string> | default = ""]

# Add random suffix to the node name.
# CLI flag: -memberlist.randomize-node-name
[randomize_node_name: <boolean> | default = true]

# The timeout for establishing a connection with a remote node, and for
# read/write operations. Uses memberlist LAN defaults if 0.
# CLI flag: -memberlist.stream-timeout
[stream_timeout: <duration> | default = 0s]

# Multiplication factor used when sending out messages (factor * log(N+1)).
# CLI flag: -memberlist.retransmit-factor
[retransmit_factor: <int> | default = 0]

# How often to use pull/push sync. Uses memberlist LAN defaults if 0.
# CLI flag: -memberlist.pullpush-interval
[pull_push_interval: <duration> | default = 0s]

# How often to gossip. Uses memberlist LAN defaults if 0.
# CLI flag: -memberlist.gossip-interval
[gossip_interval: <duration> | default = 0s]

# How many nodes to gossip to. Uses memberlist LAN defaults if 0.
# CLI flag: -memberlist.gossip-nodes
[gossip_nodes: <int> | default = 0]

# How long to keep gossiping to dead nodes, to give them chance to refute their
# death. Uses memberlist LAN defaults if 0.
# CLI flag: -memberlist.gossip-to-dead-nodes-time
[gossip_to_dead_nodes_time: <duration> | default = 0s]

# How soon can dead node's name be reclaimed with new address. Defaults to 0,
# which is disabled.
# CLI flag: -memberlist.dead-node-reclaim-time
[dead_node_reclaim_time: <duration> | default = 0s]

# Other cluster members to join. Can be specified multiple times. It can be an
# IP, hostname or an entry specified in the DNS Service Discovery format (see
# https://grafana.com/docs/mimir/latest/operators-guide/configuring/about-dns-service-discovery/
# for more details).
# CLI flag: -memberlist.join
[join_members: <list of string> | default = ]

# Min backoff duration to join other cluster members.
# CLI flag: -memberlist.min-join-backoff
[min_join_backoff: <duration> | default = 1s]

# Max backoff duration to join other cluster members.
# CLI flag: -memberlist.max-join-backoff
[max_join_backoff: <duration> | default = 1m]

# Max number of retries to join other cluster members.
# CLI flag: -memberlist.max-join-retries
[max_join_retries: <int> | default = 10]

# If this node fails to join memberlist cluster, abort.
# CLI flag: -memberlist.abort-if-join-fails
[abort_if_cluster_join_fails: <boolean> | default = true]

# If not 0, how often to rejoin the cluster. Occasional rejoin can help to fix
# the cluster split issue, and is harmless otherwise. For example when using
# only few components as a seed nodes (via -memberlist.join), then it's
# recommended to use rejoin. If -memberlist.join points to dynamic service that
# resolves to all gossiping nodes (eg. Kubernetes headless service), then rejoin
# is not needed.
# CLI flag: -memberlist.rejoin-interval
[rejoin_interval: <duration> | default = 0s]

# How long to keep LEFT ingesters in the ring.
# CLI flag: -memberlist.left-ingesters-timeout
[left_ingesters_timeout: <duration> | default = 5m]

# Timeout for leaving memberlist cluster.
# CLI flag: -memberlist.leave-timeout
[leave_timeout: <duration> | default = 5s]

# IP address to listen on for gossip messages. Multiple addresses may be
# specified. Defaults to 0.0.0.0
# CLI flag: -memberlist.bind-addr
[bind_addr: <list of string> | default = ]

# Port to listen on for gossip messages.
# CLI flag: -memberlist.bind-port
[bind_port: <int> | default = 7946]

# Timeout used when connecting to other nodes to send packet.
# CLI flag: -memberlist.packet-dial-timeout
[packet_dial_timeout: <duration> | default = 5s]

# Timeout for writing 'packet' data.
# CLI flag: -memberlist.packet-write-timeout
[packet_write_timeout: <duration> | default = 5s]
```

## storage_config

The `storage_config` block configures one of many possible stores for both the
index and chunks. Which configuration to be picked should be defined in schema_config
block.

```yaml
# Configures storing chunks in AWS. Required options only required when aws is
# present.
aws:
  # S3 or S3-compatible endpoint URL with escaped Key and Secret encoded.
  # If only region is specified as a host, the proper endpoint will be deduced.
  # Use inmemory:///<bucket-name> to use a mock in-memory implementation.
  # CLI flag: -s3.url
  [s3: <string>]

  # Set to true to force the request to use path-style addressing
  # CLI flag: -s3.force-path-style
  [s3forcepathstyle: <boolean> | default = false]

  # Comma separated list of bucket names to evenly distribute chunks over.
  # Overrides any buckets specified in s3.url flag
  # CLI flag: -s3.buckets
  [bucketnames: <string> | default = ""]

  # S3 Endpoint to connect to.
  # CLI flag: -s3.endpoint
  [endpoint: <string> | default = ""]

  # AWS region to use.
  # CLI flag: -s3.region
  [region: <string> | default = ""]

  # AWS Access Key ID.
  # CLI flag: -s3.access-key-id
  [access_key_id: <string> | default = ""]

  # AWS Secret Access Key.
  # CLI flag: -s3.secret-access-key
  [secret_access_key: <string> | default = ""]

  # Disable https on S3 connection.
  # CLI flag: -s3.insecure
  [insecure: <boolean> | default = false]

  # Enable AES256 AWS Server Side Encryption.
  # CLI flag: -s3.sse-encryption
  [sse_encryption: <boolean> | default = false]

  http_config:
    # The maximum amount of time an idle connection will be held open.
    # CLI flag: -s3.http.idle-conn-timeout
    [idle_conn_timeout: <duration> | default = 1m30s]

    # If non-zero, specifies the amount of time to wait for a server's response
    # headers after fully writing the request.
    # CLI flag: -s3.http.response-header-timeout
    [response_header_timeout: <duration> | default = 0s]

    # Set to true to skip verifying the certificate chain and hostname.
    # CLI flag: -s3.http.insecure-skip-verify
    [insecure_skip_verify: <boolean> | default = false]

    # Path to the trusted CA file that signed the SSL certificate of the S3
    # endpoint.
    # CLI flag: -s3.http.ca-file
    [ca_file: <string> | default = ""]

  # Configures back off when s3 get Object.
  backoff_config:
    # Minimum duration to back off.
    # CLI flag: -s3.backoff-min-period
    [min_period: <duration> | default = 100ms]

    # The duration to back off.
    # CLI flag: -s3.backoff-max-period
    [max_period: <duration> | default = 3s]

    # Number of times to back off and retry before failing.
    # CLI flag: -s3.backoff-retries
    [max_retries: <int> | default = 5]

  # Configure the DynamoDB connection
  dynamodb:
    # URL for DynamoDB with escaped Key and Secret encoded. If only region is specified as a
    # host, the proper endpoint will be deduced. Use inmemory:///<bucket-name> to
    # use a mock in-memory implementation.
    # CLI flag: -dynamodb.url
    dynamodb_url: <string>

    # DynamoDB table management requests per-second limit.
    # CLI flag: -dynamodb.api-limit
    [api_limit: <float> | default = 2.0]

    # DynamoDB rate cap to back off when throttled.
    # CLI flag: -dynamodb.throttle-limit
    [throttle_limit: <float> | default = 10.0]

    # Metrics-based autoscaling configuration.
    metrics:
      # Use metrics-based autoscaling via this Prometheus query URL.
      # CLI flag: -metrics.url
      [url: <string>]

      # Queue length above which we will scale up capacity.
      # CLI flag: -metrics.target-queue-length
      [target_queue_length: <int> | default = 100000]

      # Scale up capacity by this multiple
      # CLI flag: -metrics.scale-up-factor
      [scale_up_factor: <float64> | default = 1.3]

      # Ignore throttling below this level (rate per second)
      # CLI flag: -metrics.ignore-throttle-below
      [ignore_throttle_below: <float64> | default = 1]

    # Number of chunks to group together to parallelise fetches (0 to disable)
    # CLI flag: -dynamodb.chunk-gang-size
    [chunk_gang_size: <int> | default = 10]

    # Max number of chunk get operations to start in parallel.
    # CLI flag: -dynamodb.chunk.get-max-parallelism
    [chunk_get_max_parallelism: <int> | default = 32]

# Configures storing indexes in Bigtable. Required fields only required
# when bigtable is defined in config.
bigtable:
  # BigTable project ID
   # CLI flag: -bigtable.project
  project: <string>

  # BigTable instance ID
  # CLI flag: -bigtable.instance
  instance: <string>

  # Configures the gRPC client used to connect to Bigtable.
  # The CLI flags prefix for this block config is: bigtable
  [grpc_client_config: <grpc_client_config>]

# Configures storing chunks in GCS. Required fields only required
# when gcs is defined in config.
gcs:
  # Name of GCS bucket to put chunks in.
  # CLI flag: -gcs.bucketname
  bucket_name: <string>

  # The size of the buffer that the GCS client uses for each PUT request. 0
  # to disable buffering.
  # CLI flag: -gcs.chunk-buffer-size
  [chunk_buffer_size: <int> | default = 0]

  # The duration after which the requests to GCS should be timed out.
  # CLI flag: -gcs.request-timeout
  [request_timeout: <duration> | default = 0s]

# Configures storing chunks and/or the index in Cassandra
cassandra:
  # Comma-separated hostnames or IPs of Cassandra instances
  # CLI flag: -cassandra.addresses
  addresses: <string>

  # Port that cassandra is running on
  # CLI flag: -cassandra.port
  [port: <int> | default = 9042]

  # Keyspace to use in Cassandra
  # CLI flag: -cassandra.keyspace
  keyspace: <string>

  # Consistency level for Cassandra
  # CLI flag: -cassandra.consistency
  [consistency: <string> | default = "QUORUM"]

  # Replication factor to use in Cassandra.
  # CLI flag: -cassandra.replication-factor
  [replication_factor: <int> | default = 1]

  # Instruct the Cassandra driver to not attempt to get host
  # info from the system.peers table.
  # CLI flag: -cassandra.disable-initial-host-lookup
  [disable_initial_host_lookup: <boolean> | default = false]

  # Use SSL when connecting to Cassandra instances.
  # CLI flag: -cassandra.ssl
  [SSL: <boolean> | default = false]

  # Require SSL certificate validation when SSL is enabled.
  # CLI flag: -cassandra.host-verification
  [host_verification: <boolean> | default = true]

  # Path to certificate file to verify the peer when SSL is enabled.
  # CLI flag: -cassandra.ca-path
  [CA_path: <string>]

  # Enable password authentication when connecting to Cassandra.
  # CLI flag: -cassandra.auth
  [auth: <boolean> | default = false]

  # Username for password authentication when auth is true.
  # CLI flag: -cassandra.username
  [username: <string>]

  # Password for password authentication when auth is true.
  # CLI flag: -cassandra.password
  [password: <string>]

  # Timeout when connecting to Cassandra.
  # CLI flag: -cassandra.timeout
  [timeout: <duration> | default = 600ms]

  # Initial connection timeout during initial dial to server.
  # CLI flag: -cassandra.connect-timeout
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
# required when boltdb is present in the configuration.
boltdb:
  # Location of BoltDB index files.
  # CLI flag: -boltdb.dir
  directory: <string>

# Configures storing the chunks on the local file system. Required
# fields only required when filesystem is present in the configuration.
filesystem:
  # Directory to store chunks in.
  # CLI flag: -local.chunk-directory
  directory: <string>

# Configures storing index in an Object Store(GCS/S3/Azure/Swift/Filesystem) in the form of
# boltdb files.
# Required fields only required when boltdb-shipper is defined in config.
boltdb_shipper:
  # Directory where ingesters would write boltdb files which would then be
  # uploaded by shipper to configured storage
  # CLI flag: -boltdb.shipper.active-index-directory
  [active_index_directory: <string> | default = ""]

  # Shared store for keeping boltdb files. Supported types: gcs, s3, azure,
  # filesystem
  # CLI flag: -boltdb.shipper.shared-store
  [shared_store: <string> | default = ""]

  # Cache location for restoring boltDB files for queries
  # CLI flag: -boltdb.shipper.cache-location
  [cache_location: <string> | default = ""]

  # TTL for boltDB files restored in cache for queries
  # CLI flag: -boltdb.shipper.cache-ttl
  [cache_ttl: <duration> | default = 24h]

  # Resync downloaded files with the storage
  # CLI flag: -boltdb.shipper.resync-interval
  [resync_interval: <duration> | default = 5m]

  # Number of days of index to be kept downloaded for queries. Works only with
  # tables created with 24h period.
  # CLI flag: -boltdb.shipper.query-ready-num-days
  [query_ready_num_days: <int> | default = 0]

  index_gateway_client:
    # "Hostname or IP of the Index Gateway gRPC server.
    # CLI flag: -boltdb.shipper.index-gateway-client.server-address
    [server_address: <string> | default = ""]

    # Configures the gRPC client used to connect to the Index Gateway gRPC server.
    # The CLI flags prefix for this block config is: boltdb.shipper.index-gateway-client
    [grpc_client_config: <grpc_client_config>]

# Cache validity for active index entries. Should be no higher than
# the chunk_idle_period in the ingester settings.
# CLI flag: -store.index-cache-validity
[index_cache_validity: <duration> | default = 5m]

# Disable broad index queries which results in reduced cache usage and faster query performance at the expense of
# somewhat higher QPS on the index store.
# CLI flag: -store.disable-broad-index-queries
[disable_broad_index_queries: <bool> | default = false]

# The maximum number of chunks to fetch per batch.
# CLI flag: -store.max-chunk-batch-size
[max_chunk_batch_size: <int> | default = 50]

# Config for how the cache for index queries should be built.
# The CLI flags prefix for this block config is: store.index-cache-read
index_queries_cache_config: <cache_config>
```

## chunk_store_config

The `chunk_store_config` block configures how chunks will be cached and how long
to wait before saving them to the backing store.

```yaml
# The cache configuration for storing chunks
# The CLI flags prefix for this block config is: store.chunks-cache
[chunk_cache_config: <cache_config>]

# The cache configuration for deduplicating writes
# The CLI flags prefix for this block config is: store.index-cache-write
[write_dedupe_cache_config: <cache_config>]

# The minimum time between a chunk update and being saved
# to the store.
[min_chunk_age: <duration>]

# Cache index entries older than this period. Default is disabled.
# CLI flag: -store.cache-lookups-older-than
[cache_lookups_older_than: <duration>]

# Limit how long back data can be queried. Default is disabled.
# This should always be set to a value less than or equal to
# what is set in `table_manager.retention_period` .
# CLI flag: -store.max-look-back-period
[max_look_back_period: <duration>]
```

## cache_config

The `cache_config` block configures how Loki will cache requests, chunks, and
the index to a backing cache store.

<span style="background-color:#f3f973;">The memcached configuration variable addresses is experimental.</span>

```yaml
# Enable in-memory cache.
# CLI flag: -<prefix>.cache.enable-fifocache
[enable_fifocache: <boolean>]

# The default validity of entries for caches unless overridden.
# NOTE In Loki versions older than 1.4.0 this was "defaul_validity".
# CLI flag: -<prefix>.default-validity
[default_validity: <duration>]

# Configures the background cache when memcached is used.
background:
  # How many goroutines to use to write back to memcached.
  # CLI flag: -<prefix>.background.write-back-concurrency
  [writeback_goroutines: <int> | default = 10]

  # How many chunks to buffer for background write back to memcached.
  # CLI flagL -<prefix>.background.write-back-buffer
  [writeback_buffer: <int> = 10000]

# Configures memcached settings.
memcached:
  # Configures how long keys stay in memcached.
  # CLI flag: -<prefix>.memcached.expiration
  expiration: <duration>

  # Configures how many keys to fetch in each batch request.
  # CLI flag: -<prefix>.memcached.batchsize
  batch_size: <int> | default = 1024

  # Maximum active requests to memcached.
  # CLI flag: -<prefix>.memcached.parallelism
  [parallelism: <int> | default = 100]

# Configures how to connect to one or more memcached servers.
memcached_client:
  # The hostname to use for memcached services when caching chunks. If
  # empty, no memcached will be used. A SRV lookup will be used.
  # CLI flag: -<prefix>.memcached.hostname
  [host: <string>]

  # SRV service used to discover memcached servers.
  # CLI flag: -<prefix>.memcached.service
  [service: <string> | default = "memcached"]

  # (Experimental) Comma-separated addresses list in DNS Service Discovery format:
  # https://grafana.com/docs/mimir/latest/operators-guide/configuring/about-dns-service-discovery/
  # CLI flag: -<prefix>.memcached.addresses
  [addresses: <string> | default = ""]

  # Maximum time to wait before giving up on memcached requests.
  # CLI flag: -<prefix>.memcached.timeout
  [timeout: <duration> | default = 100ms]

  # The maximum number of idle connections in the memcached client pool.
  # CLI flag: -<prefix>.memcached.max-idle-conns
  [max_idle_conns: <int> | default = 16]

  # The period with which to poll the DNS for memcached servers.
  # CLI flag: -<prefix>.memcached.update-interval
  [update_interval: <duration> | default = 1m]

  # Whether or not to use a consistent hash to discover multiple memcached servers.
  # CLI flag: -<prefix>.memcached.consistent-hash
  [consistent_hash: <boolean> | default = true]

  # Trip the circuit breaker after this number of consecutive dial failures.
  # A value of 0 disables the circuit breaker.
  # CLI flag: -<prefix>.memcached.circuit-breaker-consecutive-failures
  [circuit_breaker_consecutive_failures: <int> | default = 10]

  # Duration the circuit breaker remains open after tripping.
  # If set to 0, the duration is 60 seconds.
  # CLI flag: -<prefix>.memcached.circuit-breaker-timeout
  [circuit_breaker_timeout: <duration> | default = 10s]

  # Reset the circuit breaker counts after this duration.
  # A value of 0 never resets the circuit breaker.
  # CLI flag: -<prefix>.memcached.circuit-breaker-interval
  [circuit_breaker_interval: <duration> | default = 10s]

  # The maximum size of an item stored in memcached.
  # Bigger items are not stored. If set to 0, no maximum size is enforced.
  # CLI flag: -<prefix>.memcached.max-item-size
  [max_item_size: <int> | default = 0]

redis:
  # Redis Server or Cluster configuration endpoint to use for caching. A comma-separated list of endpoints
  # for Redis Cluster or Redis Sentinel. If empty, no redis will be used.
  # CLI flag: -<prefix>.redis.endpoint
  [endpoint: <string>]

  # Redis Sentinel master name. An empty string for Redis Server or Redis Cluster.
  # CLI flag: -<prefix>.redis.master-name
  [master_name: <string>]

  # Maximum time to wait before giving up on redis requests.
  # CLI flag: -<prefix>.redis.timeout
  [timeout: <duration> | default = 500ms]

  # How long keys stay in the redis.
  # CLI flag: -<prefix>.redis.expiration
  [expiration: <duration> | default = 0s]

  # Database index.
  # CLI flag: -<prefix>.redis.db
  [db: <int> | default = 0]

  # Maximum number of connections in the pool.
  # CLI flag: -<prefix>.redis.pool-size
  [pool_size: <int> | default = 0]

  # Password to use when connecting to redis.
  # CLI flag: -<prefix>.redis.password
  [password: <string>]

  # Enables connecting to redis with TLS.
  # CLI flag: -<prefix>.redis.tls-enabled
  [tls_enabled: <boolean> | default = false]

  # Skip validating server certificate.
  # CLI flag: -<prefix>.redis.tls-insecure-skip-verify
  [tls_insecure_skip_verify: <boolean> | default = false]

  # Close connections after remaining idle for this duration.
  # If the value is zero, then idle connections are not closed.
  # CLI flag: -<prefix>.redis.idle-timeout
  [idle_timeout: <duration> | default = 0s]

  # Close connections older than this duration. If the value is zero, then
  # the pool does not close connections based on age.
  # CLI flag: -<prefix>.redis.max-connection-age
  [max_connection_age: <duration> | default = 0s]

fifocache:
  # Maximum memory size of the cache in bytes. A unit suffix (KB, MB, GB) may be
  # applied.
  # CLI flag: -<prefix>.fifocache.max-size-bytes
  [max_size_bytes: <string> | default = "1GB"]

  # Maximum number of entries in the cache.
  # CLI flag: -<prefix>.fifocache.max-size-items
  [max_size_items: <int> | default = 0]

  # Deprecated: The expiry duration for the cache. Use `-<prefix>.fifocache.ttl`.
  # The default value of 0 disables expiration.
  # CLI flag: -<prefix>.fifocache.duration
  [validity: <duration>]

  # The time for items to live in the cache before those items are purged.
  # The value of 0 disables auto-expiration.
  # CLI flag: -<prefix>.fifocache.ttl
  [ttl: <duration> | default = 1h]
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
# In YYYY-MM-DD format, for example: 2018-04-15.
[from: <daytime>]

# store and object_store below affect which <storage_config> key is
# used.

# Which store to use for the index. Either aws, aws-dynamo, gcp, bigtable, bigtable-hashed,
# cassandra, boltdb or boltdb-shipper.
store: <string>

# Which store to use for the chunks. Either aws, azure, gcp,
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

## compactor

The `compactor` block configures the compactor component. This component periodically
compacts index shards to more performant forms.

```yaml
# Directory where files can be downloaded for compaction.
# CLI flag: -boltdb.shipper.compactor.working-directory
[working_directory: <string>]

# The shared store used for storing boltdb files.
# Supported types: gcs, s3, azure, swift, filesystem, bos.
# CLI flag: -boltdb.shipper.compactor.shared-store
[shared_store: <string>]

# Prefix to add to object keys in shared store.
# Path separator(if any) should always be a '/'.
# Prefix should never start with a separator but should always end with it.
# CLI flag: -boltdb.shipper.compactor.shared-store.key-prefix
[shared_store_key_prefix: <string> | default = "index/"]

# Interval at which to re-run the compaction operation (or retention if enabled).
# CLI flag: -boltdb.shipper.compactor.compaction-interval
[compaction_interval: <duration> | default = 10m]

# (Experimental) Activate custom (per-stream,per-tenant) retention.
# CLI flag: -boltdb.shipper.compactor.retention-enabled
[retention_enabled: <boolean> | default = false]

# Delay after which chunks will be fully deleted during retention.
# CLI flag: -boltdb.shipper.compactor.retention-delete-delay
[retention_delete_delay: <duration> | default = 2h]

# The total amount of worker to use to delete chunks.
# CLI flag: -boltdb.shipper.compactor.retention-delete-worker-count
[retention_delete_worker_count: <int> | default = 150]

# Allow cancellation of delete request until duration after they are created.
# Data would be deleted only after delete requests have been older than this duration.
# Ideally this should be set to at least 24h.
# CLI flag: -boltdb.shipper.compactor.delete-request-cancel-period
[delete_request_cancel_period: <duration> | default = 24h]

# Which deletion mode to use. Supported values are: disabled,
# whole-stream-deletion, filter-only, filter-and-delete
# CLI flag: -boltdb.shipper.compactor.deletion-mode
[deletion_mode: <string> | default = "whole-stream-deletion"]

# Maximum number of tables to compact in parallel.
# While increasing this value, please make sure compactor has enough disk space
# allocated to be able to store and compact as many tables.
# CLI flag: -boltdb.shipper.compactor.max-compaction-parallelism
[max_compaction_parallelism: <int> | default = 1]

# The hash ring configuration used by compactors to elect a single instance for running compactions
# The CLI flags prefix for this block config is: boltdb.shipper.compactor.ring
[compactor_ring: <ring>]
```

## limits_config

The `limits_config` block configures global and per-tenant limits in Loki.

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
# CLI flag: -distributor.ingestion-rate-limit-strategy
[ingestion_rate_strategy: <string> | default = "global"]

# Per-user ingestion rate limit in sample size per second. Units in MB.
# CLI flag: -distributor.ingestion-rate-limit-mb
[ingestion_rate_mb: <float> | default = 4]

# Per-user allowed ingestion burst size (in sample size). Units in MB.
# The burst size refers to the per-distributor local rate limiter even in the
# case of the "global" strategy, and should be set at least to the maximum logs
# size expected in a single push request.
# CLI flag: -distributor.ingestion-burst-size-mb
[ingestion_burst_size_mb: <int> | default = 6]

# Maximum length of a label name.
# CLI flag: -validation.max-length-label-name
[max_label_name_length: <int> | default = 1024]

# Maximum length of a label value.
# CLI flag: -validation.max-length-label-value
[max_label_value_length: <int> | default = 2048]

# Maximum number of label names per series.
# CLI flag: -validation.max-label-names-per-series
[max_label_names_per_series: <int> | default = 30]

# Whether or not old samples will be rejected.
# CLI flag: -validation.reject-old-samples
[reject_old_samples: <boolean> | default = true]

# Maximum accepted sample age before rejecting.
# CLI flag: -validation.reject-old-samples.max-age
[reject_old_samples_max_age: <duration> | default = 168h]

# Duration for a table to be created/deleted before/after it's
# needed. Samples won't be accepted before this time.
# CLI flag: -validation.create-grace-period
[creation_grace_period: <duration> | default = 10m]

# Enforce every sample has a metric name.
# CLI flag: -validation.enforce-metric-name
[enforce_metric_name: <boolean> | default = true]

# Maximum line size on ingestion path. Example: 256kb.
# There is no limit when unset or set to 0.
# CLI flag: -distributor.max-line-size
[max_line_size: <string> | default = 0 ]

# Truncate log lines when they exceed max_line_size.
# CLI flag: -distributor.max-line-size-truncate
[max_line_size_truncate: <boolean> | default = false ]

# Fudge the log line timestamp during ingestion when it's the same as the previous entry for the same stream
# When enabled, if a log line in a push request has the same timestamp as the previous line
# for the same stream, one nanosecond is added to the log line. This will preserve the received
# order of log lines with the exact same timestamp when they are queried by slightly altering
# their stored timestamp. NOTE: this is imperfect because Loki accepts out of order writes
# and another push request for the same stream could contain duplicate timestamps to existing
# entries and they will not be fudged.
# CLI flag: -validation.fudge-duplicate-timestamps
[fudge_duplicate_timestamp: <boolean> | default = false ]

# Maximum number of log entries that will be returned for a query.
# CLI flag: -validation.max-entries-limit
[max_entries_limit_per_query: <int> | default = 5000 ]

# Maximum number of active streams per user, per ingester. 0 to make it unlimited.
# CLI flag: -ingester.max-streams-per-user
[max_streams_per_user: <int> | default 0]

# Maximum number of active streams per user, across the cluster. 0 to disable.
# When the global limit is enabled, each ingester is configured with a dynamic
# local limit based on the replication factor and the current number of healthy
# ingesters, and is kept updated whenever the number of ingesters change.
# CLI flag: -ingester.max-global-streams-per-user
[max_global_streams_per_user: <int> | default = 5000]

# When true, out-of-order writes are accepted.
# CLI flag: -ingester.unordered-writes
[unordered_writes: <boolean> | default = true]

# Maximum number of chunks that can be fetched by a single query.
# CLI flag: -store.query-chunk-limit
[max_chunks_per_query: <int> | default = 2000000]

# The limit to length of chunk store queries. 0 to disable.
# CLI flag: -store.max-query-length
[max_query_length: <duration> | default = 721h]

# Maximum number of queries that will be scheduled in parallel by the frontend.
# CLI flag: -querier.max-query-parallelism
[max_query_parallelism: <int> | default = 32]

# Limit the maximum of unique series that is returned by a metric query.
# When the limit is reached an error is returned.
# CLI flag: -querier.max-query-series
[max_query_series: <int> | default = 500]

# Cardinality limit for index queries.
# CLI flag: -store.cardinality-limit
[cardinality_limit: <int> | default = 100000]

# Maximum number of stream matchers per query.
# CLI flag: -querier.max-streams-matcher-per-query
[max_streams_matchers_per_query: <int> | default = 1000]

# Maximum number of concurrent tail requests.
# CLI flag: -querier.max-concurrent-tail-requests
[max_concurrent_tail_requests: <int> | default = 10]

# Duration to delay the evaluation of rules to ensure.
# CLI flag: -ruler.evaluation-delay-duration
[ruler_evaluation_delay_duration: <duration> | default = 0s]

# Maximum number of rules per rule group per-tenant. 0 to disable.
# CLI flag: -ruler.max-rules-per-rule-group
[ruler_max_rules_per_rule_group: <int> | default = 0]

# Maximum number of rule groups per-tenant. 0 to disable.
# CLI flag: -ruler.max-rule-groups-per-tenant
[ruler_max_rule_groups_per_tenant: <int> | default = 0]

# Retention to apply for the store, if the retention is enable on the compactor side.
# CLI flag: -store.retention
[retention_period: <duration> | default = 744h]

# Per-stream retention to apply, if the retention is enable on the compactor side.
# Example:
# retention_stream:
# - selector: '{namespace="dev"}'
#   priority: 1
#   period: 24h
# - selector: '{container="nginx"}'
#   priority: 1
#   period: 744h
# Selector is a Prometheus labels matchers that will apply the `period` retention only if
# the stream is matching. In case multiple stream are matching, the highest
# priority will be picked. If no rule is matched the `retention_period` is used.
[retention_stream: <array> | default = none]

# Feature renamed to 'runtime configuration', flag deprecated in favor of -runtime-config.file
# (runtime_config.file in YAML).
# CLI flag: -limits.per-user-override-config
[per_tenant_override_config: <string>]

# Feature renamed to 'runtime configuration'; flag deprecated in favor of
# -runtime-config.reload-period (runtime_config.period in YAML).
# CLI flag: -limits.per-user-override-period
[per_tenant_override_period: <duration> | default = 10s]

# Most recent allowed cacheable result per-tenant, to prevent caching very recent results that
# might still be in flux.
# CLI flag: -frontend.max-cache-freshness
[max_cache_freshness_per_query: <duration> | default = 1m]

# Maximum number of queriers that can handle requests for a single tenant. If
# set to 0 or value higher than number of available queriers, *all* queriers
# will handle requests for the tenant. Each frontend (or query-scheduler, if
# used) will select the same set of queriers for the same tenant (given that all
# queriers are connected to all frontends / query-schedulers). This option only
# works with queriers connecting to the query-frontend / query-scheduler, not
# when using downstream URL.
# CLI flag: -frontend.max-queriers-per-tenant
[max_queriers_per_tenant: <int> | default = 0]

# Maximum byte rate per second per stream,
# also expressible in human readable forms (1MB, 256KB, etc).
# CLI flag: -ingester.per-stream-rate-limit
[per_stream_rate_limit: <string|int> | default = "3MB"]

# Maximum burst bytes per stream,
# also expressible in human readable forms (1MB, 256KB, etc).
# This is how far above the rate limit a stream can "burst" before the stream is limited.
# CLI flag: -ingester.per-stream-rate-limit-burst
[per_stream_rate_limit_burst: <string|int> | default = "15MB"]

# Limit how far back in time series data and metadata can be queried,
# up until lookback duration ago.
# This limit is enforced in the query frontend, the querier and the ruler.
# If the requested time range is outside the allowed range, the request will not fail,
# but will be modified to only query data within the allowed time range.
# The default value of 0 does not set a limit.
# CLI flag: -querier.max-query-lookback
[max_query_lookback: <duration> | default = 0]

# Disable recording rules remote-write.
[ruler_remote_write_disabled: <boolean> | default = false]

# The URL of the endpoint to send samples to.
[ruler_remote_write_url: <string>]

# Timeout for requests to the remote write endpoint.
[ruler_remote_write_timeout: <duration>]

# Custom HTTP headers to be sent along with each remote write request.
# Be aware that headers that are set by Loki itself can't be overwritten.
[ruler_remote_write_headers: <headers>]

# List of remote write relabel configurations.
[ruler_remote_write_relabel_configs: <relabel_config>]

# Number of samples to buffer per shard before we block reading of more
# samples from the WAL. It is recommended to have enough capacity in each
# shard to buffer several requests to keep throughput up while processing
# occasional slow remote requests.
[ruler_remote_write_queue_capacity: <int>]

# Minimum number of shards, i.e. amount of concurrency.
[ruler_remote_write_queue_min_shards: <int>]

# Maximum number of shards, i.e. amount of concurrency.
[ruler_remote_write_queue_max_shards: <int>]

# Maximum number of samples per send.
[ruler_remote_write_queue_max_samples_per_send: <int>]

# Maximum time a sample will wait in buffer.
[ruler_remote_write_queue_batch_send_deadline: <duration>]

# Initial retry delay. Gets doubled for every retry.
[ruler_remote_write_queue_min_backoff: <duration>]

# Maximum retry delay.
[ruler_remote_write_queue_max_backoff: <duration>]
# Retry upon receiving a 429 status code from the remote-write storage.
# This is experimental and might change in the future.
[ruler_remote_write_queue_retry_on_ratelimit: <boolean>]

# Limit queries that can be sharded.
# Queries within the time range of now and now minus this sharding lookback
# are not sharded. The default value of 0s disables the lookback, causing
# sharding of all queries at all times.
# CLI flag: -frontend.min-sharding-lookback
[min_sharding_lookback: <duration> | default = 0s]

# Split queries by an interval and execute in parallel, any value less than zero disables it.
# This also determines how cache keys are chosen when result caching is enabled
# CLI flag: -querier.split-queries-by-interval
[split_queries_by_interval: <duration> | default = 30m]
```

### grpc_client_config

The `grpc_client_config` block configures a client connection to a gRPC service.

```yaml
# The maximum size in bytes the client can receive.
# CLI flag: -<prefix>.grpc-max-recv-msg-size
[max_recv_msg_size: <int> | default = 104857600]

# The maximum size in bytes the client can send.
# CLI flag: -<prefix>.grpc-max-send-msg-size
[max_send_msg_size: <int> | default = 16777216]

# Use compression when sending messages. Supported values are: 'gzip', 'snappy',
# and '' (disable compression).
# CLI flag: -<prefix>.grpc-compression
[grpc_compression: <string> | default = '']

# Rate limit for gRPC client. 0 is disabled.
# CLI flag: -<prefix>.grpc-client-rate-limit
[rate_limit: <float> | default = 0]

# Rate limit burst for gRPC client.
# CLI flag: -<prefix>.grpc-client-rate-limit-burst
[rate_limit_burst: <int> | default = 0]

# Enable backoff and retry when a rate limit is hit.
# CLI flag: -<prefix>.backoff-on-ratelimits
[backoff_on_ratelimits: <boolean> | default = false]

# Configures backoff when enabled.
backoff_config:
  # Minimum delay when backing off.
  # CLI flag: -<prefix>.backoff-min-period
  [min_period: <duration> | default = 100ms]

  # The maximum delay when backing off.
  # CLI flag: -<prefix>.backoff-max-period
  [max_period: <duration> | default = 10s]

  # Number of times to backoff and retry before failing.
  # CLI flag: -<prefix>.backoff-retries
  [max_retries: <int> | default = 10]
```

## table_manager

The `table_manager` block configures the Loki table-manager.

```yaml
# Master 'off-switch' for table capacity updates, e.g. when troubleshooting.
# CLI flag: -table-manager.throughput-updates-disabled
[throughput_updates_disabled: <boolean> | default = false]

# Master 'on-switch' for table retention deletions.
# CLI flag: -table-manager.retention-deletes-enabled
[retention_deletes_enabled: <boolean> | default = false]

# How far back tables will be kept before they are deleted. 0s disables
# deletion. The retention period must be a multiple of the index / chunks
# table "period" (see period_config).
# CLI flag: -table-manager.retention-period
[retention_period: <duration> | default = 0s]

# Period with which the table manager will poll for tables.
# CLI flag: -table-manager.poll-interval
[poll_interval: <duration> | default = 2m]

# Duration a table will be created before it is needed.
# CLI flag: -table-manager.periodic-table.grace-period
[creation_grace_period: <duration> | default = 10m]

# Configures management of the index tables for DynamoDB.
# The CLI flags prefix for this block config is: table-manager.index-table
index_tables_provisioning: <provision_config>

# Configures management of the chunk tables for DynamoDB.
# The CLI flags prefix for this block config is: table-manager.chunk-table
chunk_tables_provisioning: <provision_config>
```

### provision_config

The `provision_config` block configures provisioning capacity for DynamoDB.

```yaml
# Enables on-demand throughput provisioning for the storage
# provider, if supported. Applies only to tables which are not autoscaled.
# CLI flag: -<prefix>.enable-ondemand-throughput-mode
[enable_ondemand_throughput_mode: <boolean> | default = false]

# DynamoDB table default write throughput.
# CLI flag: -<prefix>.write-throughput
[provisioned_write_throughput: <int> | default = 3000]

# DynamoDB table default read throughput.
# CLI flag: -<prefix>.read-throughput
[provisioned_read_throughput: <int> | default = 300]

# Enables on-demand throughput provisioning for the storage provide,
# if supported. Applies only to tables which are not autoscaled.
# CLI flag: -<prefix>.inactive-enable-ondemand-throughput-mode
[enable_inactive_throughput_on_demand_mode: <boolean> | default = false]

# DynamoDB table write throughput for inactive tables.
# CLI flag: -<prefix>.inactive-write-throughput
[inactive_write_throughput: <int> | default = 1]

# DynamoDB table read throughput for inactive tables.
# CLI flag: -<prefix>.inactive-read-throughput
[inactive_read_throughput: <int> | default = 300]

# Active table write autoscale config.
# The CLI flags prefix for this block config is: -<prefix>.write-throughput
[write_scale: <auto_scaling_config>]

# Inactive table write autoscale config.
# The CLI flags prefix for this block config is: -<prefix>.inactive-write-throughput
[inactive_write_scale: <auto_scaling_config>]

# Number of last inactive tables to enable write autoscale.
# CLI flag: -<prefix>.enable-ondemand-throughput-mode
[inactive_write_scale_lastn: <int>]

# Active table read autoscale config.
# The CLI flags prefix for this block config is: -<prefix>.read-throughput
[read_scale: <auto_scaling_config>]

# Inactive table read autoscale config.
# The CLI flags prefix for this block config is: -<prefix>.inactive-read-throughput
[inactive_read_scale: <auto_scaling_config>]

# Number of last inactive tables to enable read autoscale.
# CLI flag: -<prefix>.enable-ondemand-throughput-mode
[inactive_read_scale_lastn: <int>]
```

#### auto_scaling_config

The `auto_scaling_config` block configures autoscaling for DynamoDB.

```yaml
# Whether or not autoscaling should be enabled.
# CLI flag: -<prefix>.scale.enabled
[enabled: <boolean>: default = false]

# AWS AutoScaling role ARN.
# CLI flag: -<prefix>.scale.role-arn
[role_arn: <string>]

# DynamoDB minimum provision capacity.
# CLI flag: -<prefix>.scale.min-capacity
[min_capacity: <int> | default = 3000]

# DynamoDB maximum provision capacity.
# CLI flag: -<prefix>.scale.max-capacity
[max_capacity: <int> | default = 6000]

# DynamoDB minimum seconds between each autoscale up.
# CLI flag: -<prefix>.scale.out-cooldown
[out_cooldown: <int> | default = 1800]

# DynamoDB minimum seconds between each autoscale down.
# CLI flag: -<prefix>.scale.in-cooldown
[in_cooldown: <int> | default = 1800]

# DynamoDB target ratio of consumed capacity to provisioned capacity.
# CLI flag: -<prefix>.scale.target-value
[target: <float> | default = 80]
```

## tracing

The `tracing` block configures tracing for Jaeger. Currently limited to disable auto-configuration per [environment variables](https://www.jaegertracing.io/docs/1.16/client-features/) only.

```yaml
# Whether or not tracing should be enabled.
# CLI flag: -tracing.enabled
[enabled: <boolean>: default = true]
```

## common

The `common` block sets common definitions to be shared by different components.
This way, one doesn't have to replicate configuration in multiple places.

```yaml
# A common storage configuration to be used by the different Loki components.
[storage: <storage>]

# When defined, the given prefix will be present in front of the endpoint paths.
[path_prefix: <string>]

# How many times incoming data should be replicated to the ingester component.
[replication_factor: <int> | default = 3]

# When true, the ingester, compactor, and query_scheduler ring tokens will be saved
# to files in the path_prefix directory. Loki will error if you set this to true
# and path_prefix is empty.
[persist_tokens: <boolean>: default = false]

# A common list of net interfaces used internally to look for addresses.
# If a more specific "instance_interface_names" is set, this is ignored.
# If "instance_interface_names" under the common ring section is configured,
# this common "instance_interface_names" is only applied to the frontend, but not for
# ring related components (ex: distributor, ruler, etc).
[instance_interface_names: <list of string> | default = [<private network interfaces>]]

# A common address used by Loki components to advertise their address.
# If a more specific "instance_addr" is set, this is ignored.
# If "instance_addr" under the common ring section is configured, this common "instance_addr"
# is only applied to the frontend, but not for ring related components (ex: distributor, ruler, etc).
[instance_addr: <string>]

# A common ring configuration to be used by all Loki rings.
# If a common ring is given, its values are used to define any undefined ring values.
# For instance, you can expect the `heartbeat_period` defined in the common section
# to be used by the distributor's ring, but only if the distributor's ring itself
# doesn't have a `heartbeat_period` set.
[ring: <ring>]
```

## analytics

The `analytics` block configures the reporting of Loki analytics to grafana.com

```yaml
# By default, Loki will send anonymous, but uniquely-identifiable usage and configuration
# analytics to Grafana Labs. These statistics are sent to https://stats.grafana.org/
#
# Statistics help us better understand how Loki is used, and they show us performance
# levels for most users. This helps us prioritize features and documentation.
# For more information on what's sent, look at
# https://github.com/grafana/loki/blob/main/pkg/usagestats/stats.go
# Refer to the buildReport method to see what goes into a report.
#
# When true, enables usage reporting.
# CLI flag: -reporting.enabled
[reporting_enabled: <boolean>: default = true]
```

### storage

The common `storage` block defines a common storage to be reused by different
components as a way to facilitate storage configuration.
If any specific configuration for an object storage client have been provided elsewhere in the configuration file, the specific configuration will supersede the common storage configuration.

```yaml
# Configures Azure as the common storage.
[azure: <azure_storage_config>]

# Configures GCS as the common storage.
[gcs: <gcs_storage_config>]

# Configures S3 as the common storage.
[s3: <s3_storage_config>]

# Configures Swift as the common storage.
[swift: <swift_storage_config>]

# Configures a (local) file system as the common storage.
[filesystem: <filesystem>]

# Configures Baidu Object Storage (BOS) as the common storage.
[bos: <bos_storage_config>]

# The `hedging_config` configures how to hedge requests for the storage.
[hedging: <hedging_config>]

```

### filesystem

The common `filesystem` block configures a local file system as a general
storage for various types of data generated by Loki.

```yaml
# File system directory to be used for chunks storage.
[chunks_directory: <filename> | default = ""]

# File system directory to be used for rules storage.
[rules_directory: <filename> | default = ""]
```

### ring

The common `ring` block defines a ring configuration used by a Loki component.

```yaml
# The key-value store used to share the hash ring across multiple instances.
kvstore:
  # Backend storage to use for the ring. Supported values are: consul, etcd,
  # inmemory, memberlist, multi.
  # CLI flag: -<prefix>.store
  [store: <string> | default = "memberlist"]

  # The prefix for the keys in the store. Should end with a /.
  # CLI flag: -<prefix>.prefix
  [prefix: <string> | default = "collectors/"]

  # The consul_config configures the consul client.
  [consul: <consul_config>]

  # The etcd_config configures the etcd client.
  [etcd: <etcd_config>]

  multi:
    # Primary backend storage used by multi-client.
    # CLI flag: -<prefix>.multi.primary
    [primary: <string> | default = ""]

    # Secondary backend storage used by multi-client.
    # CLI flag: -<prefix>.multi.secondary
    [secondary: <string> | default = ""]

    # Mirror writes to secondary store.
    # CLI flag: -<prefix>.multi.mirror-enabled
    [mirror_enabled: <boolean> | default = false]

    # Timeout for storing value to secondary store.
    # CLI flag: -<prefix>.multi.mirror-timeout
    [mirror_timeout: <duration> | default = 2s]

# Interval between heartbeats sent to the ring. 0 = disabled.
# CLI flag: -<prefix>.heartbeat-period
[heartbeat_period: <duration> | default = 15s]

# The heartbeat timeout after which store gateways are considered unhealthy
# within the ring. 0 = never (timeout disabled). This option needs be set both
# on the store-gateway and querier when running in microservices mode.
# CLI flag: -<prefix>.heartbeat-timeout
[heartbeat_timeout: <duration> | default = 1m]

# File path where tokens are stored. If empty, tokens are neither stored at
# shutdown nor restored at startup.
# CLI flag: -<prefix>.tokens-file-path
[tokens_file_path: <string> | default = ""]

# True to enable zone-awareness and replicate blocks across different
# availability zones.
# CLI flag: -<prefix>.zone-awareness-enabled
[zone_awareness_enabled: <boolean> | default = false]

# Name of network interface to read addresses from.
# CLI flag: -<prefix>.instance-interface-names
[instance_interface_names: <list of string> | default = [<private network interfaces>]]

# IP address to advertise in the ring.
# CLI flag: -<prefix>.instance-addr
[instance_addr: <list of string> | default = first from instance_interface_names]

# Port to advertise in the ring
# CLI flag: -<prefix>.instance-port
[instance_port: <list of string> | default = server.grpc-listen-port]

# Instance ID to register in the ring.
# CLI flag: -<prefix>.instance-id
[instance_id: <list of string> | default = os.Hostname()]

# The availability zone where this instance is running. Required if
# zone-awareness is enabled.
# CLI flag: -<prefix>.instance-availability-zone
[instance_availability_zone: <string> | default = ""]
```

## Runtime Configuration file

Loki has a concept of "runtime config" file, which is simply a file that is reloaded while Loki is running. It is used by some Loki components to allow operator to change some aspects of Loki configuration without restarting it. File is specified by using `-runtime-config.file=<filename>` flag and reload period (which defaults to 10 seconds) can be changed by `-runtime-config.reload-period=<duration>` flag. Previously this mechanism was only used by limits overrides, and flags were called `-limits.per-user-override-config=<filename>` and `-limits.per-user-override-period=10s` respectively. These are still used, if `-runtime-config.file=<filename>` is not specified.

At the moment, two components use runtime configuration: limits and multi KV store.

Options for runtime configuration reload can also be configured via YAML:

```yaml
# Configuration file to periodically check and reload.
[file: <string>: default = empty]

# How often to check the file.
[period: <duration>: default 10s]
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

## Accept out-of-order writes

Since the beginning of Loki, log entries had to be written to Loki in order
by time.
This limitation has been lifted.
Out-of-order writes are enabled globally by default, but can be disabled/enabled
on a cluster or per-tenant basis.

- To disable out-of-order writes for all tenants,
place in the `limits_config` section:

    ```
    limits_config:
        unordered_writes: false
    ```

- To disable out-of-order writes for specific tenants,
configure a runtime configuration file:

    ```
    runtime_config: overrides.yaml
    ```

    In the `overrides.yaml` file, add `unordered_writes` for each tenant
    permitted to have out-of-order writes:

    ```
    overrides:
      "tenantA":
        unordered_writes: false
    ```

How far into the past accepted out-of-order log entries may be
is configurable with `max_chunk_age`.
`max_chunk_age` defaults to 1 hour.
Loki calculates the earliest time that out-of-order entries may have
and be accepted with

```
time_of_most_recent_line - (max_chunk_age/2)
```

Log entries with timestamps that are after this earliest time are accepted.
Log entries further back in time return an out-of-order error.

For example, if `max_chunk_age` is 2 hours
and the stream `{foo="bar"}` has one entry at `8:00`,
Loki will accept data for that stream as far back in time as `7:00`.
If another log line is written at `10:00`,
Loki will accept data for that stream as far back in time as `9:00`.
