package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/pointer"

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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
					},
				},
				Tenants: map[string]lokiv1.LimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
						},
					},
				},
			},
		},
		Overrides: map[string]LokiOverrides{
			"test-a": {
				Limits: lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						MaxGlobalStreamsPerTenant: 1,
						IngestionBurstSize:        5,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxChunksPerQuery: 1000000,
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
      timeout: 10s
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
      timeout: 10s
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
      timeout: 10s
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
frontend:
  tail_proxy_url: http://loki-querier-http-lokistack-dev.default.svc.cluster.local:3100
  compress_responses: true
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  retention_period: 15d
  retention_stream:
  - selector: '{environment="development"}'
    priority: 1
    period: 3d
  max_cache_freshness_per_query: 10m
  per_stream_rate_limit: 3MB
  per_stream_rate_limit_burst: 15MB
  split_queries_by_interval: 30m
  query_timeout: 1m
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
				Tenants: map[string]lokiv1.LimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
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
				Limits: lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             2,
						IngestionBurstSize:        5,
						MaxGlobalStreamsPerTenant: 1,
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxChunksPerQuery: 1000000,
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 2m
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
      timeout: 10s
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "2m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  tail_tls_config:
    tls_cert_path: /var/run/tls/http/tls.crt
    tls_key_path: /var/run/tls/http/tls.key
    tls_ca_path: /var/run/tls/ca.pem
    tls_server_name: querier-http.svc
    tls_cipher_suites: cipher1,cipher2
    tls_min_version: VersionTLS12
  compress_responses: true
  max_outstanding_per_tenant: 256
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
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 2m
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
      timeout: 10s
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "2m",
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
								ServerName:         pointer.String("custom-servername"),
								CertPath:           pointer.String("custom/path"),
								KeyPath:            pointer.String("custom/key"),
								CAPath:             pointer.String("custom/CA"),
								InsecureSkipVerify: pointer.Bool(false),
							},
							BasicAuth: BasicAuth{
								Username: pointer.String("user"),
								Password: pointer.String("pass"),
							},
							HeaderAuth: HeaderAuth{
								CredentialsFile: pointer.String("cred/file"),
								Type:            pointer.String("auth"),
								Credentials:     pointer.String("creds"),
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
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
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
frontend_worker:
  frontend_address: loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 104857600
  match_max_concurrent: true
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
  max_transfer_retries: 0
  wal:
    enabled: true
    dir: /tmp/wal
    replay_memory_ceiling: 2500
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
  enforce_metric_name: false
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
  query_timeout: 1m
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
    shared_store: s3
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
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
						QueryTimeout:            "1m",
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
			IngesterMemoryRequest: 5000,
		},
		ObjectStorage: storage.Options{
			SharedStore: lokiv1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
			Schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-01",
				},
			},
		},
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
