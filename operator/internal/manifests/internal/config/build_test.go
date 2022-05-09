package config

import (
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/stretchr/testify/require"
)

func TestBuild_ConfigAndRuntimeConfig_NoRuntimeConfigGenerated(t *testing.T) {
	expCfg := `
---
auth_enabled: true
chunk_store_config:
  chunk_cache_config:
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
common:
  storage:
    s3:
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
      s3forcepathstyle: true
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
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
      heartbeat_timeout: 1m
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
memberlist:
  abort_if_cluster_join_fails: true
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
    timeout: 3m
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  query_timeout: 1m
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
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
  http_server_idle_timeout: 120s
  http_server_write_timeout: 1m
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
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
					},
					QueryLimits: &lokiv1beta1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
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
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
		},
		EnableRemoteReporting: true,
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
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
common:
  storage:
    s3:
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
      s3forcepathstyle: true
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
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
      heartbeat_timeout: 1m
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
memberlist:
  abort_if_cluster_join_fails: true
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
    timeout: 3m
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  query_timeout: 1m
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
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
  http_server_idle_timeout: 120s
  http_server_write_timeout: 1m
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
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
					},
					QueryLimits: &lokiv1beta1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
					},
				},
				Tenants: map[string]lokiv1beta1.LimitsTemplateSpec{
					"test-a": {
						IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
							IngestionRate:             2,
							IngestionBurstSize:        5,
							MaxGlobalStreamsPerTenant: 1,
						},
						QueryLimits: &lokiv1beta1.QueryLimitSpec{
							MaxChunksPerQuery: 1000000,
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
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
		},
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}

func TestBuild_ConfigAndRuntimeConfig_CreateLokiConfigFailed(t *testing.T) {
	opts := Options{
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
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
		FrontendWorker: Address{
			FQDN: "loki-query-frontend-grpc-lokistack-dev.default.svc.cluster.local",
			Port: 9095,
		},
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
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
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
common:
  storage:
    s3:
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
      s3forcepathstyle: true
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
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
      heartbeat_timeout: 1m
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
memberlist:
  abort_if_cluster_join_fails: true
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
    timeout: 3m
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  query_timeout: 1m
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
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
  http_server_idle_timeout: 120s
  http_server_write_timeout: 1m
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
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
					},
					QueryLimits: &lokiv1beta1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
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
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
					HeaderBearerToken: "supersecret",
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
		},
		EnableRemoteReporting: true,
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
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
common:
  storage:
    s3:
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
      s3forcepathstyle: true
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
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
      heartbeat_timeout: 1m
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
memberlist:
  abort_if_cluster_join_fails: true
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
    timeout: 3m
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  query_timeout: 1m
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
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
  http_server_idle_timeout: 120s
  http_server_write_timeout: 1m
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
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
					},
					QueryLimits: &lokiv1beta1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
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
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
		},
		EnableRemoteReporting: true,
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
    enable_fifocache: true
    fifocache:
      max_size_bytes: 500MB
common:
  storage:
    s3:
      s3: http://test.default.svc.cluster.local.:9000
      bucketnames: loki
      region: us-east
      access_key_id: test
      secret_access_key: test123
      s3forcepathstyle: true
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
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      replication_factor: 1
      heartbeat_timeout: 1m
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
memberlist:
  abort_if_cluster_join_fails: true
  bind_port: 7946
  join_members:
    - loki-gossip-ring-lokistack-dev.default.svc.cluster.local:7946
  max_join_backoff: 1m
  max_join_retries: 10
  min_join_backoff: 1s
querier:
  engine:
    max_look_back_period: 30s
    timeout: 3m
  extra_query_delay: 0s
  max_concurrent: 2
  query_ingesters_within: 3h
  query_timeout: 1m
  tail_max_duration: 1h
query_range:
  align_queries_with_step: true
  cache_results: true
  max_retries: 5
  results_cache:
    cache:
      enable_fifocache: true
      fifocache:
        max_size_bytes: 500MB
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
        separator: ";"
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
  http_server_idle_timeout: 120s
  http_server_write_timeout: 1m
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
		Stack: lokiv1beta1.LokiStackSpec{
			ReplicationFactor: 1,
			Limits: &lokiv1beta1.LimitsSpec{
				Global: &lokiv1beta1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1beta1.IngestionLimitSpec{
						IngestionRate:             4,
						IngestionBurstSize:        6,
						MaxLabelNameLength:        1024,
						MaxLabelValueLength:       2048,
						MaxLabelNamesPerSeries:    30,
						MaxGlobalStreamsPerTenant: 0,
						MaxLineSize:               256000,
					},
					QueryLimits: &lokiv1beta1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: 5000,
						MaxChunksPerQuery:       2000000,
						MaxQuerySeries:          500,
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
		GossipRing: Address{
			FQDN: "loki-gossip-ring-lokistack-dev.default.svc.cluster.local",
			Port: 7946,
		},
		Querier: Address{
			FQDN: "loki-querier-http-lokistack-dev.default.svc.cluster.local",
			Port: 3100,
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
				RelabelConfigs: []RemoteWriteRelabelConfig{
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
			SharedStore: lokiv1beta1.ObjectStorageSecretS3,
			S3: &storage.S3StorageConfig{
				Endpoint:        "http://test.default.svc.cluster.local.:9000",
				Region:          "us-east",
				Buckets:         "loki",
				AccessKeyID:     "test",
				AccessKeySecret: "test123",
			},
		},
		EnableRemoteReporting: true,
	}
	cfg, rCfg, err := Build(opts)
	require.NoError(t, err)
	require.YAMLEq(t, expCfg, string(cfg))
	require.YAMLEq(t, expRCfg, string(rCfg))
}
