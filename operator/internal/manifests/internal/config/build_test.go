package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestBuild_ConfigAndRuntimeConfig_NoRuntimeConfigGenerated(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 536870912
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 0,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_BothGenerated(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: false
`
	expRCfg := `
---
overrides:
  test-a:
    ingestion_rate_mb: 2
    ingestion_burst_size_mb: 5
    max_global_streams_per_user: 1
    max_chunks_per_query: 1000000
    blocked_queries:
    - pattern: ""
      hash: 12345
      types: metric,limited
    - pattern: .*prod.*
      regex: true
    - pattern: ""
      types: metric
    - pattern: sum(rate({env="prod"}[1m]))
    - pattern: '{kubernetes_namespace_name="my-app"}'
    - pattern: ""
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								MaxChunksPerQuery: 1000000,
							},
							Blocked: []lokiv1.BlockedQuerySpec{
								{
									Hash:  12345,
									Types: lokiv1.BlockedQueryTypes{lokiv1.BlockedQueryMetric, lokiv1.BlockedQueryLimited},
								},
								{
									Pattern: ".*prod.*",
									Regex:   true,
								},
								{
									Types: lokiv1.BlockedQueryTypes{lokiv1.BlockedQueryMetric},
								},
								{
									Pattern: `sum(rate({env="prod"}[1m]))`,
								},
								{
									Pattern: `{kubernetes_namespace_name="my-app"}`,
								},
								{
									Pattern: "",
								},
							},
						},
					},
				},
			},
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.PerTenantLimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						MaxGlobalStreamsPerTenant: 1,
						IngestionBurstSize:        5,
					},
					QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
						QueryLimitSpec: lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
						},
						Blocked: []lokiv1.BlockedQuerySpec{
							{
								Hash:  12345,
								Types: lokiv1.BlockedQueryTypes{lokiv1.BlockedQueryMetric, lokiv1.BlockedQueryLimited},
							},
							{
								Pattern: ".*prod.*",
								Regex:   true,
							},
							{
								Types: lokiv1.BlockedQueryTypes{lokiv1.BlockedQueryMetric},
							},
							{
								Pattern: `sum(rate({env="prod"}[1m]))`,
							},
							{
								Pattern: `{kubernetes_namespace_name="my-app"}`,
							},
							{
								Pattern: "",
							},
						},
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers: []string{"boltdb"},
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_CreateLokiConfigFailed(t *testing.T) {
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					// making it nil so that the template is not generated and error is returned
					QueryLimits: nil,
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers: []string{"boltdb"},
	}
	cfg, rCfg, err := Build(opts)
	require.Error(t, err)
	require.Empty(t, cfg)
	require.Empty(t, rCfg)
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithHeaderAuthorization(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      authorization:
        type: bearer
        credentials: supersecret
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:        "http://proxy.through.me",
					FollowRedirects: true,
					BearerToken:     "supersecret",
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithBasicAuthorization(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      basic_auth:
        username: user
        password: passwd
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:          "http://proxy.through.me",
					FollowRedirects:   true,
					BasicAuthUsername: "user",
					BasicAuthPassword: "passwd",
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithRelabelConfigs(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      write_relabel_configs:
      - source_labels: ["labela","labelb"]
        regex: "ALERTS.*"
        action: "drop"
        separator: "\\"
        replacement: "$1"
      - source_labels: ["labelc","labeld"]
        regex: "ALERTS.*"
        action: "drop"
        separator: ""
        replacement: "$1"
        target_label: "labeld"
        modulus: 123
      basic_auth:
        username: user
        password: passwd
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:          "http://proxy.through.me",
					FollowRedirects:   true,
					BasicAuthUsername: "user",
					BasicAuthPassword: "passwd",
				},
				RelabelConfigs: []RelabelConfig{
					{
						SourceLabels: []string{"labela", "labelb"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Separator:    "\\",
						Replacement:  "$1",
					},
					{
						SourceLabels: []string{"labelc", "labeld"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Replacement:  "$1",
						TargetLabel:  "labeld",
						Modulus:      123,
					},
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithRetention(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
  retention_enabled: true
  retention_delete_delay: 4h
  retention_delete_worker_count: 50
  delete_request_store: s3
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  retention_period: 15d
  retention_stream:
  - selector: '{environment="development"}'
    priority: 1
    period: 3d
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: false
`
	expRCfg := `
---
overrides:
  test-a:
    ingestion_rate_mb: 2
    ingestion_burst_size_mb: 5
    max_global_streams_per_user: 1
    max_chunks_per_query: 1000000
    retention_period: 7d
    retention_stream:
    - period: 15d
      priority: 1
      selector: "{environment=\"production\"}"
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
					Retention: &lokiv1.RetentionLimitSpec{
						Days: 15,
						Streams: []*lokiv1.RetentionStreamSpec{
							{
								Days:     3,
								Priority: 1,
								Selector: `{environment="development"}`,
							},
						},
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								MaxChunksPerQuery: 1000000,
							},
						},
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 7,
							Streams: []*lokiv1.RetentionStreamSpec{
								{
									Days:     15,
									Priority: 1,
									Selector: `{environment="production"}`,
								},
							},
						},
					},
				},
			},
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.PerTenantLimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						IngestionBurstSize:        5,
						MaxGlobalStreamsPerTenant: 1,
					},
					QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
						QueryLimitSpec: lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
						},
					},
					Retention: &lokiv1.RetentionLimitSpec{
						Days: 7,
						Streams: []*lokiv1.RetentionStreamSpec{
							{
								Days:     15,
								Priority: 1,
								Selector: `{environment="production"}`,
							},
						},
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers: []string{"boltdb"},
		Retention: RetentionOptions{
			Enabled:           true,
			DeleteWorkerCount: 50,
		},
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithAlertRelabelConfigs(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 2m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  alert_relabel_configs:
  - source_labels: ["source1", "source2"]
    regex: "ALERTS.*"
    action: "drop"
    separator: "\\"
    replacement: "$1"
  - source_labels: ["source3", "source4"]
    regex: "ALERTS.*"
    action: "keep"
    separator: ""
    replacement: "$1"
    target_label: "target"
    modulus: 42
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      write_relabel_configs:
      - source_labels: ["labela","labelb"]
        regex: "ALERTS.*"
        action: "drop"
        separator: "\\"
        replacement: "$1"
      - source_labels: ["labelc","labeld"]
        regex: "ALERTS.*"
        action: "drop"
        separator: ""
        replacement: "$1"
        target_label: "labeld"
        modulus: 123
      basic_auth:
        username: user
        password: passwd
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "2m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
				RelabelConfigs: []RelabelConfig{
					{
						SourceLabels: []string{"source1", "source2"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Separator:    "\\",
						Replacement:  "$1",
					},
					{
						SourceLabels: []string{"source3", "source4"},
						Regex:        "ALERTS.*",
						Action:       "keep",
						Replacement:  "$1",
						TargetLabel:  "target",
						Modulus:      42,
					},
				},
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:          "http://proxy.through.me",
					FollowRedirects:   true,
					BasicAuthUsername: "user",
					BasicAuthPassword: "passwd",
				},
				RelabelConfigs: []RelabelConfig{
					{
						SourceLabels: []string{"labela", "labelb"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Separator:    "\\",
						Replacement:  "$1",
					},
					{
						SourceLabels: []string{"labelc", "labeld"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Replacement:  "$1",
						TargetLabel:  "labeld",
						Modulus:      123,
					},
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithTLS(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://s3.us-east.amazonaws.com
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  tail_tls_config:
    tls_cert_path: /var/run/tls/http/tls.crt
    tls_key_path: /var/run/tls/http/tls.key
    tls_ca_path: /var/run/tls/ca.pem
    tls_server_name: querier-http.svc
    tls_cipher_suites: cipher1,cipher2
    tls_min_version: VersionTLS12
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
    tls_enabled: true
    tls_cert_path: /var/run/tls/grpc/tls.crt
    tls_key_path: /var/run/tls/grpc/tls.key
    tls_ca_path: /var/run/tls/ca.pem
    tls_server_name: query-frontend-grpc.svc
    tls_cipher_suites: cipher1,cipher2
    tls_min_version: VersionTLS12
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
    tls_enabled: true
    tls_cert_path: /var/run/tls/grpc/tls.crt
    tls_key_path: /var/run/tls/grpc/tls.key
    tls_ca_path: /var/run/tls/ca.pem
    tls_server_name: ingester-grpc.svc
    tls_cipher_suites: cipher1,cipher2
    tls_min_version: VersionTLS12
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
compactor_grpc_client:
  tls_enabled: true
  tls_cert_path: /var/run/tls/grpc/tls.crt
  tls_key_path: /var/run/tls/grpc/tls.key
  tls_ca_path: /var/run/tls/ca.pem
  tls_server_name: compactor-grpc.svc
  tls_cipher_suites: cipher1,cipher2
  tls_min_version: VersionTLS12
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper

internal_server:
  enable: true
  http_listen_address: ""
  tls_min_version: VersionTLS12
  tls_cipher_suites: cipher1,cipher2
  http_tls_config:
    cert_file: /var/run/tls/http/tls.crt
    key_file: /var/run/tls/http/tls.key
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  tls_min_version: VersionTLS12
  tls_cipher_suites: cipher1,cipher2
  http_tls_config:
    cert_file: /var/run/tls/http/tls.crt
    key_file: /var/run/tls/http/tls.key
    client_auth_type: RequireAndVerifyClientCert
    client_ca_file: /var/run/tls/ca.pem
  grpc_tls_config:
    cert_file: /var/run/tls/grpc/tls.crt
    key_file: /var/run/tls/grpc/tls.key
    client_auth_type: RequireAndVerifyClientCert
    client_ca_file: /var/run/tls/ca.pem
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
      grpc_client_config:
        tls_enabled: true
        tls_cert_path: /var/run/tls/grpc/tls.crt
        tls_key_path: /var/run/tls/grpc/tls.key
        tls_ca_path: /var/run/tls/ca.pem
        tls_server_name: index-gateway-grpc.svc
        tls_cipher_suites: cipher1,cipher2
        tls_min_version: VersionTLS12
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Gates: configv1.FeatureGates{
			HTTPEncryption: true,
			GRPCEncryption: true,
		},
		TLS: TLSOptions{
			Ciphers:       []string{"cipher1", "cipher2"},
			MinTLSVersion: "VersionTLS12",
			Paths: TLSFilePaths{
				CA: "/var/run/tls/ca.pem",
				GRPC: TLSCertPath{
					Certificate: "/var/run/tls/grpc/tls.crt",
					Key:         "/var/run/tls/grpc/tls.key",
				},
				HTTP: TLSCertPath{
					Certificate: "/var/run/tls/http/tls.crt",
					Key:         "/var/run/tls/http/tls.key",
				},
			},
			ServerNames: TLSServerNames{
				GRPC: GRPCServerNames{
					Compactor:     "compactor-grpc.svc",
					IndexGateway:  "index-gateway-grpc.svc",
					Ingester:      "ingester-grpc.svc",
					QueryFrontend: "query-frontend-grpc.svc",
					Ruler:         "ruler-grpc.svc",
				},
				HTTP: HTTPServerNames{
					Querier: "querier-http.svc",
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint: "http://s3.us-east.amazonaws.com",
				Region:   "us-east",
				Buckets:  "loki",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithAlertmanagerOverrides(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 2m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  alert_relabel_configs:
  - source_labels: ["source1", "source2"]
    regex: "ALERTS.*"
    action: "drop"
    separator: "\\"
    replacement: "$1"
  - source_labels: ["source3", "source4"]
    regex: "ALERTS.*"
    action: "keep"
    separator: ""
    replacement: "$1"
    target_label: "target"
    modulus: 42
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      write_relabel_configs:
      - source_labels: ["labela","labelb"]
        regex: "ALERTS.*"
        action: "drop"
        separator: "\\"
        replacement: "$1"
      - source_labels: ["labelc","labeld"]
        regex: "ALERTS.*"
        action: "drop"
        separator: ""
        replacement: "$1"
        target_label: "labeld"
        modulus: 123
      basic_auth:
        username: user
        password: passwd
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
  test-a:
    ruler_alertmanager_config:
      external_url: http://alert.you/later
      external_labels:
        specialkey: specialvalue
      alertmanager_url: http://specialhost
      enable_alertmanager_v2: true
      enable_alertmanager_discovery: true
      alertmanager_refresh_interval: 2m
      notification_queue_capacity: 2000
      notification_timeout: 2m
      alertmanager_client:
        tls_cert_path: "custom/path"
        tls_key_path: "custom/key"
        tls_ca_path: "custom/CA"
        tls_server_name: "custom-servername"
        tls_insecure_skip_verify: false
        basic_auth_password: "pass"
        basic_auth_username: "user"
        credentials: "creds"
        credentials_file: "cred/file"
        type: "auth"
      alert_relabel_configs:
      - source_labels: ["specialsource"]
        regex: "ALERTS.*"
        action: "drop"
        separator: "\\"
        replacement: "$2"
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "2m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Ruler: RulerOverrides{
					AlertManager: &AlertManagerConfig{
						Hosts:       "http://specialhost",
						ExternalURL: "http://alert.you/later",
						ExternalLabels: map[string]string{
							"specialkey": "specialvalue",
						},
						EnableV2:           true,
						EnableDiscovery:    true,
						RefreshInterval:    "2m",
						QueueCapacity:      2000,
						Timeout:            "2m",
						ForOutageTolerance: "20m",
						ForGracePeriod:     "10m",
						ResendDelay:        "4m",
						RelabelConfigs: []RelabelConfig{
							{
								SourceLabels: []string{"specialsource"},
								Regex:        "ALERTS.*",
								Action:       "drop",
								Separator:    "\\",
								Replacement:  "$2",
							},
						},
						Notifier: &NotifierConfig{
							TLS: TLSConfig{
								ServerName:         ptr.To("custom-servername"),
								CertPath:           ptr.To("custom/path"),
								KeyPath:            ptr.To("custom/key"),
								CAPath:             ptr.To("custom/CA"),
								InsecureSkipVerify: ptr.To(false),
							},
							BasicAuth: BasicAuth{
								Username: ptr.To("user"),
								Password: ptr.To("pass"),
							},
							HeaderAuth: HeaderAuth{
								CredentialsFile: ptr.To("cred/file"),
								Type:            ptr.To("auth"),
								Credentials:     ptr.To("creds"),
							},
						},
					},
				},
			},
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",

			AlertManager: &AlertManagerConfig{
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
				RelabelConfigs: []RelabelConfig{
					{
						SourceLabels: []string{"source1", "source2"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Separator:    "\\",
						Replacement:  "$1",
					},
					{
						SourceLabels: []string{"source3", "source4"},
						Regex:        "ALERTS.*",
						Action:       "keep",
						Replacement:  "$1",
						TargetLabel:  "target",
						Modulus:      42,
					},
				},
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:          "http://proxy.through.me",
					FollowRedirects:   true,
					BasicAuthUsername: "user",
					BasicAuthPassword: "passwd",
				},
				RelabelConfigs: []RelabelConfig{
					{
						SourceLabels: []string{"labela", "labelb"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Separator:    "\\",
						Replacement:  "$1",
					},
					{
						SourceLabels: []string{"labelc", "labeld"},
						Regex:        "ALERTS.*",
						Action:       "drop",
						Replacement:  "$1",
						TargetLabel:  "labeld",
						Modulus:      123,
					},
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithHashRingSpec(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_addr: ${HASH_RING_INSTANCE_ADDR}
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_addr: ${HASH_RING_INSTANCE_ADDR}
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstanceAddr:         "${HASH_RING_INSTANCE_ADDR}",
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithHashRingSpec_EnableIPv6(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_addr: ${HASH_RING_INSTANCE_ADDR}
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    enable_inet6: true
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_addr: ${HASH_RING_INSTANCE_ADDR}
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			EnableIPv6:           true,
			InstanceAddr:         "${HASH_RING_INSTANCE_ADDR}",
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithReplicationSpec(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
    zone_awareness_enabled: true
    instance_availability_zone: ${INSTANCE_AVAILABILITY_ZONE}
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:                   9095,
			BindPort:                       7946,
			MembersDiscoveryAddr:           "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			EnableInstanceAvailabilityZone: true,
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithS3SSEKMS(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      sse:
        type: SSE-KMS
        kms_key_id: test
        kms_encryption_context: |
          ${AWS_SSE_KMS_ENCRYPTION_CONTEXT}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: false
`
	expRCfg := `
---
overrides:
  test-a:
    ingestion_rate_mb: 2
    ingestion_burst_size_mb: 5
    max_global_streams_per_user: 1
    max_chunks_per_query: 1000000
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								MaxChunksPerQuery: 1000000,
							},
						},
					},
				},
			},
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.PerTenantLimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						MaxGlobalStreamsPerTenant: 1,
						IngestionBurstSize:        5,
					},
					QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
						QueryLimitSpec: lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
						},
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,

				SSE: storage.S3SSEConfig{
					Type:                 storage.SSEKMSType,
					KMSKeyID:             "test",
					KMSEncryptionContext: `{"key": "value", "another":"value1"}`,
				},
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers: []string{"boltdb"},
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithS3SSES3(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      sse:
        type: SSE-S3
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: false
`
	expRCfg := `
---
overrides:
  test-a:
    ingestion_rate_mb: 2
    ingestion_burst_size_mb: 5
    max_global_streams_per_user: 1
    max_chunks_per_query: 1000000
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								MaxChunksPerQuery: 1000000,
							},
						},
					},
				},
			},
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.PerTenantLimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						MaxGlobalStreamsPerTenant: 1,
						IngestionBurstSize:        5,
					},
					QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
						QueryLimitSpec: lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
						},
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,

				SSE: storage.S3SSEConfig{
					Type:                 storage.SSES3Type,
					KMSKeyID:             "test",
					KMSEncryptionContext: `{"key": "value", "another":"value1"}`,
				},
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers: []string{"boltdb"},
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_WithManualPerStreamRateLimits(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  per_stream_rate_limit: 3MB
  per_stream_rate_limit_burst: 15MB
  split_queries_by_interval: 30m
  tsdb_max_query_parallelism: 512
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        3,
						PerStreamRateLimitBurst:   15,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func defaultOptions() Options {
	return Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        3,
						PerStreamRateLimitBurst:   15,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
}

func TestBuild_ConfigAndRuntimeConfig_Schemas(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		schemaConfig            []lokiv1.ObjectStorageSchema
		shippers                []string
		allowStructuredMetadata bool
		expSchemaConfig         string
		expStorageConfig        string
		expStructuredMetadata   string
	}{
		{
			name: "default_config_v11_schema",
			schemaConfig: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
			shippers: []string{"boltdb"},
			expSchemaConfig: `
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper`,
			expStorageConfig: `
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095`,
			expStructuredMetadata: `
  allow_structured_metadata: false`,
		},
		{
			name: "v12_schema",
			schemaConfig: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV12,
					EffectiveDate: "2020-02-05",
				},
			},
			shippers: []string{"boltdb"},
			expSchemaConfig: `
  configs:
    - from: "2020-02-05"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v12
      store: boltdb-shipper`,
			expStorageConfig: `
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095`,
			expStructuredMetadata: `
  allow_structured_metadata: false`,
		},
		{
			name: "v13_schema",
			schemaConfig: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV13,
					EffectiveDate: "2024-01-01",
				},
			},
			allowStructuredMetadata: true,
			shippers:                []string{"tsdb"},
			expSchemaConfig: `
  configs:
    - from: "2024-01-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v13
      store: tsdb`,
			expStorageConfig: `
  tsdb_shipper:
    active_index_directory: /tmp/loki/tsdb-index
    cache_location: /tmp/loki/tsdb-cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095`,
			expStructuredMetadata: `
  allow_structured_metadata: true`,
		},
		{
			name: "multiple_schema",
			schemaConfig: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-01-01",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV12,
					EffectiveDate: "2021-01-01",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV13,
					EffectiveDate: "2024-01-01",
				},
			},
			shippers:                []string{"boltdb", "tsdb"},
			allowStructuredMetadata: true,
			expSchemaConfig: `
  configs:
    - from: "2020-01-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
    - from: "2021-01-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v12
      store: boltdb-shipper
    - from: "2024-01-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v13
      store: tsdb`,
			expStorageConfig: `
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
  tsdb_shipper:
    active_index_directory: /tmp/loki/tsdb-index
    cache_location: /tmp/loki/tsdb-cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095`,
			expStructuredMetadata: `
  allow_structured_metadata: true`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  per_stream_rate_limit: 3MB
  per_stream_rate_limit_burst: 15MB
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  ${STORAGE_STRUCTURED_METADATA}
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
${SCHEMA_CONFIG}
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
${STORAGE_CONFIG}
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
			expCfg = strings.Replace(expCfg, "${SCHEMA_CONFIG}", tc.expSchemaConfig, 1)
			expCfg = strings.Replace(expCfg, "${STORAGE_CONFIG}", tc.expStorageConfig, 1)
			expCfg = strings.Replace(expCfg, "${STORAGE_STRUCTURED_METADATA}", tc.expStructuredMetadata, 1)

			opts := defaultOptions()
			opts.ObjectStorage.Schemas = tc.schemaConfig
			opts.ObjectStorage.AllowStructuredMetadata = tc.allowStructuredMetadata
			opts.Shippers = tc.shippers

			cfg, _, err := Build(opts)
			require.NoError(t, err)
			require.YAMLEq(t, expCfg, string(cfg))
		})
	}
}

func TestBuild_ConfigAndRuntimeConfig_STS(t *testing.T) {
	objStorageConfig := storage.Options{
		SharedStore: lokiv1.ObjectStorageSecretS3,
		S3: &storage.S3StorageConfig{
			STS:     true,
			Region:  "my-region",
			Buckets: "my-bucket",
		},
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
		},
	}
	expStorageConfig := `
    s3:
      bucketnames: my-bucket
      region: my-region
      s3forcepathstyle: false`

	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
${STORAGE_CONFIG}
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  per_stream_rate_limit: 3MB
  per_stream_rate_limit_burst: 15MB
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expCfg = strings.Replace(expCfg, "${STORAGE_CONFIG}", expStorageConfig, 1)

	opts := defaultOptions()
	opts.ObjectStorage = objStorageConfig

	cfg, _, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
}

func TestBuild_ConfigAndRuntimeConfig_RulerConfigGenerated_WithAlertmanagerClient(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: false
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2020-10-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  alertmanager_client:
    tls_cert_path: "custom/path"
    tls_key_path: "custom/key"
    tls_ca_path: "custom/CA"
    tls_server_name: "custom-servername"
    tls_insecure_skip_verify: false
    basic_auth_password: "pass"
    basic_auth_username: "user"
    credentials: "creds"
    credentials_file: "cred/file"
    type: "auth"
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      authorization:
        type: bearer
        credentials: supersecret
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  boltdb_shipper:
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/index_cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				Notifier: &NotifierConfig{
					TLS: TLSConfig{
						ServerName:         ptr.To("custom-servername"),
						CertPath:           ptr.To("custom/path"),
						KeyPath:            ptr.To("custom/key"),
						CAPath:             ptr.To("custom/CA"),
						InsecureSkipVerify: ptr.To(false),
					},
					BasicAuth: BasicAuth{
						Username: ptr.To("user"),
						Password: ptr.To("pass"),
					},
					HeaderAuth: HeaderAuth{
						CredentialsFile: ptr.To("cred/file"),
						Type:            ptr.To("auth"),
						Credentials:     ptr.To("creds"),
					},
				},
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:        "http://proxy.through.me",
					FollowRedirects: true,
					BearerToken:     "supersecret",
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
		Shippers:              []string{"boltdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_OTLPConfigGenerated(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    embedded_cache:
      enabled: true
      max_size_mb: 500
common:
  storage:
    s3:
      endpoint: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: ${AWS_ACCESS_KEY_ID}
      secret_access_key: ${AWS_ACCESS_KEY_SECRET}
      s3forcepathstyle: true
  compactor_grpc_address: loki-compactor-grpc-lokistack-dev.default.svc.cluster.local:9095
  ring:
    kvstore:
      store: memberlist
    heartbeat_period: 5s
    heartbeat_timeout: 1m
    instance_port: 9095
compactor:
  compaction_interval: 2h
  working_directory: /tmp/loki/compactor
distributor:
  otlp_config:
    default_resource_attributes_as_index_labels:
    - foo.bar
    - bar.baz
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 4096
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
ingester:
  chunk_block_size: 262144
  chunk_encoding: snappy
  chunk_idle_period: 1h
  chunk_retain_period: 5m
  chunk_target_size: 2097152
  flush_op_timeout: 10m
  lifecycler:
    final_sleep: 0s
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
  max_chunk_age: 2h
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2147483648
ingester_client:
  grpc_client_config:
    max_recv_msg_size: 67108864
  remote_timeout: 1s
# NOTE: Keep the order of keys as in Loki docs
# to enable easy diffs when vendoring newer
# Loki releases.
# (See https://grafana.com/docs/loki/latest/configuration/#limits_config)
#
# Values for not exposed fields are taken from the grafana/loki production
# configuration manifests.
# (See https://github.com/grafana/loki/blob/main/production/ksonnet/loki/config.libsonnet)
limits_config:
  ingestion_rate_strategy: global
  ingestion_rate_mb: 4
  ingestion_burst_size_mb: 6
  max_label_name_length: 1024
  max_label_value_length: 2048
  max_label_names_per_series: 30
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  creation_grace_period: 10m
  # Keep max_streams_per_user always to 0 to default
  # using max_global_streams_per_user always.
  # (See https://github.com/grafana/loki/blob/main/pkg/ingester/limiter.go#L73)
  max_streams_per_user: 0
  max_line_size: 256000
  max_entries_limit_per_query: 5000
  max_global_streams_per_user: 0
  max_chunks_per_query: 2000000
  max_query_length: 721h
  max_query_parallelism: 32
  tsdb_max_query_parallelism: 512
  max_query_series: 500
  cardinality_limit: 100000
  max_streams_matchers_per_query: 1000
  max_cache_freshness_per_query: 10m
  split_queries_by_interval: 30m
  query_timeout: 1m
  volume_enabled: true
  volume_max_series: 1000
  per_stream_rate_limit: 5MB
  per_stream_rate_limit_burst: 15MB
  shard_streams:
    enabled: true
    desired_rate: 3MB
  allow_structured_metadata: true
  otlp_config:
    resource_attributes:
      ignore_defaults: true
      attributes_config:
      - action: index_label
        attributes:
        - res.foo.bar
        - res.bar.baz
        regex: .*
      - action: structured_metadata
        attributes:
        - res.service.env
        regex: .*
    scope_attributes:
    - action: index_label
      attributes:
      - scope.foo.bar
      - scope.bar.baz
      regex: .*
    - action: structured_metadata
      attributes:
      - scope.service.env
      regex: .*
    log_attributes:
    - action: index_label
      attributes:
      - log.foo.bar
      - log.bar.baz
      regex: .*
    - action: structured_metadata
      attributes:
      - log.service.env
      regex: .*
memberlist:
  abort_if_cluster_join_fails: true
  advertise_port: 7946
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 500
  parallelise_shardable_queries: true
schema_config:
  configs:
    - from: "2024-01-01"
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v13
      store: tsdb
ruler:
  enable_api: true
  enable_sharding: true
  evaluation_interval: 1m
  poll_interval: 1m
  external_url: http://alert.me/now
  external_labels:
    key1: val1
    key2: val2
  alertmanager_url: http://alerthost1,http://alerthost2
  enable_alertmanager_v2: true
  enable_alertmanager_discovery: true
  alertmanager_refresh_interval: 1m
  notification_queue_capacity: 1000
  notification_timeout: 1m
  alertmanager_client:
    tls_cert_path: "custom/path"
    tls_key_path: "custom/key"
    tls_ca_path: "custom/CA"
    tls_server_name: "custom-servername"
    tls_insecure_skip_verify: false
    basic_auth_password: "pass"
    basic_auth_username: "user"
    credentials: "creds"
    credentials_file: "cred/file"
    type: "auth"
  for_outage_tolerance: 10m
  for_grace_period: 5m
  resend_delay: 2m
  remote_write:
    enabled: true
    config_refresh_period: 1m
    client:
      name: remote-write-me
      url: http://remote.write.me
      remote_timeout: 10s
      proxy_url: http://proxy.through.me
      follow_redirects: true
      headers:
        more: foryou
        less: forme
      authorization:
        type: bearer
        credentials: supersecret
      queue_config:
        capacity: 1000
        max_shards: 100
        min_shards: 50
        max_samples_per_send: 1000
        batch_send_deadline: 10s
        min_backoff: 30ms
        max_backoff: 100ms
  wal:
    dir: /tmp/wal
    truncate_frequency: 60m
    min_age: 5m
    max_age: 4h
  rule_path: /tmp/loki
  storage:
    type: local
    local:
      directory: /tmp/rules
  ring:
    kvstore:
      store: memberlist
server:
  graceful_shutdown_timeout: 5s
  grpc_server_min_time_between_pings: '10s'
  grpc_server_ping_without_stream_allowed: true
  grpc_server_max_concurrent_streams: 1000
  grpc_server_max_recv_msg_size: 104857600
  grpc_server_max_send_msg_size: 104857600
  http_listen_port: 3100
  http_server_idle_timeout: 30s
  http_server_read_timeout: 30s
  http_server_write_timeout: 10m0s
  log_level: info
storage_config:
  tsdb_shipper:
    active_index_directory: /tmp/loki/tsdb-index
    cache_location: /tmp/loki/tsdb-cache
    cache_ttl: 24h
    resync_interval: 5m
    index_gateway_client:
      server_address: dns:///loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local:9095
tracing:
  enabled: false
analytics:
  reporting_enabled: true
`
	expRCfg := `
---
overrides:
  test-a:
    otlp_config:
      resource_attributes:
        ignore_defaults: true
        attributes_config:
        - action: index_label
          attributes:
          - res.foo.bar
          - res.bar.baz
          regex: .*
        - action: structured_metadata
          attributes:
          - res.service.env
          regex: .*
      scope_attributes:
      - action: index_label
        attributes:
        - scope.foo.bar
        - scope.bar.baz
        regex: .*
      - action: structured_metadata
        attributes:
        - scope.service.env
        regex: .*
      log_attributes:
      - action: index_label
        attributes:
        - log.foo.bar
        - log.bar.baz
        regex: .*
      - action: structured_metadata
        attributes:
        - log.service.env
        regex: .*
`
	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Factor: 1,
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
						PerStreamRateLimit:        5,
						PerStreamRateLimitBurst:   15,
						PerStreamDesiredRate:      3,
					},
					OTLP: &lokiv1.GlobalOTLPSpec{
						IndexedResourceAttributes: []string{
							"foo.bar",
							"bar.baz",
						},
						OTLPSpec: lokiv1.OTLPSpec{
							ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
								IgnoreDefaults: true,
								Attributes: []lokiv1.OTLPResourceAttributesConfigSpec{
									{
										Action: lokiv1.OTLPAttributeActionIndexLabel,
										Attributes: []string{
											"res.foo.bar",
											"res.bar.baz",
										},
										Regex: ".*",
									},
									{
										Action: lokiv1.OTLPAttributeActionStructuredMetadata,
										Attributes: []string{
											"res.service.env",
										},
										Regex: ".*",
									},
								},
							},
							ScopeAttributes: []lokiv1.OTLPAttributesSpec{
								{
									Action: lokiv1.OTLPAttributeActionIndexLabel,
									Attributes: []string{
										"scope.foo.bar",
										"scope.bar.baz",
									},
									Regex: ".*",
								},
								{
									Action: lokiv1.OTLPAttributeActionStructuredMetadata,
									Attributes: []string{
										"scope.service.env",
									},
									Regex: ".*",
								},
							},
							LogAttributes: []lokiv1.OTLPAttributesSpec{
								{
									Action: lokiv1.OTLPAttributeActionIndexLabel,
									Attributes: []string{
										"log.foo.bar",
										"log.bar.baz",
									},
									Regex: ".*",
								},
								{
									Action: lokiv1.OTLPAttributeActionStructuredMetadata,
									Attributes: []string{
										"log.service.env",
									},
									Regex: ".*",
								},
							},
						},
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
						CardinalityLimit:        100000,
						MaxVolumeSeries:         1000,
					},
				},
			},
		},
		Namespace: "test-ns",
		Name:      "test",
		Compactor: Address{
			FQDN: "loki-compactor-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: GossipRing{
			InstancePort:         9095,
			BindPort:             7946,
			MembersDiscoveryAddr: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
		},
		Querier: Address{
			Protocol: "http",
			FQDN:     "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port:     3100,
		},
		IndexGateway: Address{
			FQDN: "loki-index-gateway-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		Ruler: Ruler{
			Enabled:               true,
			RulesStorageDirectory: "/tmp/rules",
			EvaluationInterval:    "1m",
			PollInterval:          "1m",
			AlertManager: &AlertManagerConfig{
				Notifier: &NotifierConfig{
					TLS: TLSConfig{
						ServerName:         ptr.To("custom-servername"),
						CertPath:           ptr.To("custom/path"),
						KeyPath:            ptr.To("custom/key"),
						CAPath:             ptr.To("custom/CA"),
						InsecureSkipVerify: ptr.To(false),
					},
					BasicAuth: BasicAuth{
						Username: ptr.To("user"),
						Password: ptr.To("pass"),
					},
					HeaderAuth: HeaderAuth{
						CredentialsFile: ptr.To("cred/file"),
						Type:            ptr.To("auth"),
						Credentials:     ptr.To("creds"),
					},
				},
				ExternalURL: "http://alert.me/now",
				ExternalLabels: map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
				Hosts:              "http://alerthost1,http://alerthost2",
				EnableV2:           true,
				EnableDiscovery:    true,
				RefreshInterval:    "1m",
				QueueCapacity:      1000,
				Timeout:            "1m",
				ForOutageTolerance: "10m",
				ForGracePeriod:     "5m",
				ResendDelay:        "2m",
			},
			RemoteWrite: &RemoteWriteConfig{
				Enabled:       true,
				RefreshPeriod: "1m",
				Client: &RemoteWriteClientConfig{
					Name:          "remote-write-me",
					URL:           "http://remote.write.me",
					RemoteTimeout: "10s",
					Headers: map[string]string{
						"more": "foryou",
						"less": "forme",
					},
					ProxyURL:        "http://proxy.through.me",
					FollowRedirects: true,
					BearerToken:     "supersecret",
				},
				Queue: &RemoteWriteQueueConfig{
					Capacity:          1000,
					MaxShards:         100,
					MinShards:         50,
					MaxSamplesPerSend: 1000,
					BatchSendDeadline: "10s",
					MinBackOffPeriod:  "30ms",
					MaxBackOffPeriod:  "100ms",
				},
			},
		},
		StorageDirectory: "/tmp/loki",
		MaxConcurrent: MaxConcurrent{
			AvailableQuerierCPUCores: 2,
		},
		WriteAheadLog: WriteAheadLog{
			Directory:             "/tmp/wal",
			IngesterMemoryRequest: 4 * 1024 * 1024 * 1024,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:       "http://test.default.svc.cluster.local.:9000",
				Region:         "us-east",
				Buckets:        "loki",
				ForcePathStyle: true,
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV13,
					EffectiveDate: "2024-01-01",
				},
			},
			AllowStructuredMetadata: true,
		},
		Shippers:              []string{"tsdb"},
		EnableRemoteReporting: true,
		HTTPTimeouts: HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 10 * time.Minute,
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.PerTenantLimitsTemplateSpec{
					OTLP: &lokiv1.OTLPSpec{
						ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
							IgnoreDefaults: true,
							Attributes: []lokiv1.OTLPResourceAttributesConfigSpec{
								{
									Action: lokiv1.OTLPAttributeActionIndexLabel,
									Attributes: []string{
										"res.foo.bar",
										"res.bar.baz",
									},
									Regex: ".*",
								},
								{
									Action: lokiv1.OTLPAttributeActionStructuredMetadata,
									Attributes: []string{
										"res.service.env",
									},
									Regex: ".*",
								},
							},
						},
						ScopeAttributes: []lokiv1.OTLPAttributesSpec{
							{
								Action: lokiv1.OTLPAttributeActionIndexLabel,
								Attributes: []string{
									"scope.foo.bar",
									"scope.bar.baz",
								},
								Regex: ".*",
							},
							{
								Action: lokiv1.OTLPAttributeActionStructuredMetadata,
								Attributes: []string{
									"scope.service.env",
								},
								Regex: ".*",
							},
						},
						LogAttributes: []lokiv1.OTLPAttributesSpec{
							{
								Action: lokiv1.OTLPAttributeActionIndexLabel,
								Attributes: []string{
									"log.foo.bar",
									"log.bar.baz",
								},
								Regex: ".*",
							},
							{
								Action: lokiv1.OTLPAttributeActionStructuredMetadata,
								Attributes: []string{
									"log.service.env",
								},
								Regex: ".*",
							},
						},
					},
				},
			},
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}
