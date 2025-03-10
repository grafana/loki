{
  _config+: {
    namespace: error 'must define namespace',
    cluster: error 'must define cluster',
    http_listen_port: 3100,
    node_selector: null,

    create_service_monitor: false,

    replication_factor: 3,
    memcached_replicas: 3,

    grpc_server_max_msg_size: 100 << 20,  // 100MB

    query_scheduler_enabled: false,
    overrides_exporter_enabled: false,

    ingester_pvc_size: '10Gi',
    ingester_pvc_class: 'fast',

    ingester_data_disk_size: self.ingester_pvc_size,  // keep backwards compatibility
    ingester_data_disk_class: self.ingester_pvc_class,  // keep backwards compatibility

    ingester_wal_disk_size: '150Gi',
    ingester_wal_disk_class: 'fast',

    ingester_allow_multiple_replicas_on_same_node: false,

    stateful_queriers: false,
    querier_pvc_size: '10Gi',
    querier_pvc_class: 'fast',

    stateful_rulers: false,
    ruler_pvc_size: '10Gi',
    ruler_pvc_class: 'fast',

    compactor_pvc_size: '10Gi',
    compactor_pvc_class: 'fast',

    // This is the configmap created with `$._config.overrides` data
    overrides_configmap_name: 'overrides',

    // This is the configmap which will be used by workloads.
    overrides_configmap_mount_name: 'overrides',
    overrides_configmap_mount_path: '/etc/loki/overrides',

    jaeger_reporter_max_queue: 1000,

    querier: {
      // This value should be set equal to (or less than) the CPU cores of the system the querier runs.
      // A higher value will lead to a querier trying to process more requests than there are available
      // cores and will result in scheduling delays.
      concurrency: 4,

      // use_no_constraints is false by default allowing either TopologySpreadConstraints or pod antiAffinity to be configured.
      // If no_schedule_constraints is set to true, neither of the pod constraints will be applied.
      no_schedule_constraints: false,

      // If use_topology_spread is true, queriers can run on nodes already running queriers but will be
      // spread through the available nodes using a TopologySpreadConstraints with a max skew
      // of topology_spread_max_skew.
      // See: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
      // If use_topology_spread is false, queriers will not be scheduled on nodes already running queriers.
      use_topology_spread: true,
      topology_spread_max_skew: 1,
    },

    queryFrontend: {
      replicas: 2,
      shard_factor: 16,  // v10 schema shard factor
      sharded_queries_enabled: false,
    },

    storage_backend: error 'must define storage_backend. valid entries: s3,gcs',

    table_prefix: $._config.namespace,
    index_period_hours: 24,  // 1 day

    ruler_enabled: false,

    distributor: {
      // use_no_constraints is false by default allowing either TopologySpreadConstraints or pod antiAffinity to be configured.
      // If no_schedule_constraints is set to true, neither of the pod constraints will be applied.
      no_schedule_constraints: false,
      use_topology_spread: true,
      topology_spread_max_skew: 1,
    },

    // Use thanos object store clients
    use_thanos_objstore: false,

    // GCS variables
    gcs_bucket_name: error 'must specify GCS bucket name',

    // S3 variables
    s3_access_key: '',
    s3_secret_access_key: '',
    s3_address: error 'must specify s3_address',
    s3_bucket_name: error 'must specify s3_bucket_name',
    s3_bucket_region: '',
    s3_path_style: false,

    // Azure variables
    azure_container_name: error 'must specify azure_container_name',
    azure_account_name: error 'must specify azure_account_name',
    azure_account_key: '',  // secret access key, recommend setting this using environment variable

    // DNS Resolver
    dns_resolver: 'kube-dns.kube-system.svc.cluster.local',

    object_store_config:
      if $._config.storage_backend == 'gcs' then {
        gcs: {
          bucket_name: $._config.gcs_bucket_name,
        },
      } else if $._config.storage_backend == 's3' then {
        aws: {
          s3forcepathstyle: $._config.s3_path_style,
        } + (
          if $._config.s3_access_key != '' then {
            s3: 's3://' + $._config.s3_access_key + ':' + $._config.s3_secret_access_key + '@' + $._config.s3_address + '/' + $._config.s3_bucket_name,
          } else {
            s3: 's3://' + $._config.s3_address + '/' + $._config.s3_bucket_name,
          }
        ) + (
          if $._config.s3_bucket_region != '' then {
            region: $._config.s3_bucket_region,
          }
          else {}
        ),
      } else if $._config.storage_backend == 'azure' then {
        azure: {
          container_name: $._config.azure_container_name,
          account_name: $._config.azure_account_name,
        } + (
          if $._config.azure_account_key != '' then {
            account_key: $._config.azure_account_key,
          } else {}
        ),
      } else {},

    // thanos object store config
    thanos_object_store_config:
      if $._config.storage_backend == 'gcs' then {
        gcs: $._config.object_store_config.gcs,
      } else if $._config.storage_backend == 's3' then {
        s3: {
          bucket_name: $._config.s3_bucket_name,
          endpoint: $._config.s3_address,
        } + (
          if $._config.s3_access_key != '' && $._config.s3_secret_access_key != '' then {
            access_key_id: $._config.s3_access_key,
            secret_access_key: $._config.s3_secret_access_key,
          }
          else {}
        ) + (
          if $._config.s3_bucket_region != '' then {
            region: $._config.s3_bucket_region,
          }
          else {}
        ),
      } else if $._config.storage_backend == 'azure' then {
        azure: $._config.object_store_config.azure,
      } else {},

    // December 11 is when we first launched to the public.
    // Assume we can ingest logs that are 5months old.
    schema_start_date: '2018-07-11',

    commonArgs: {
      'config.file': '/etc/loki/config/config.yaml',
      'limits.per-user-override-config': '/etc/loki/overrides/overrides.yaml',
    },

    commonEnvs: [],

    loki: {
      common: {
        compactor_grpc_address: 'compactor.%s.svc.cluster.local.:9095' % [$._config.namespace],
      },
      server: {
        graceful_shutdown_timeout: '5s',
        http_server_idle_timeout: '120s',
        grpc_server_max_recv_msg_size: $._config.grpc_server_max_msg_size,
        grpc_server_max_send_msg_size: $._config.grpc_server_max_msg_size,
        grpc_server_max_concurrent_streams: 1000,
        grpc_server_ping_without_stream_allowed: true,  // https://github.com/grafana/cortex-jsonnet/pull/233
        grpc_server_min_time_between_pings: '10s',  // https://github.com/grafana/cortex-jsonnet/pull/233
        http_server_write_timeout: '1m',
        http_listen_port: $._config.http_listen_port,
      },
      frontend: {
        compress_responses: true,
        log_queries_longer_than: '5s',
      },
      frontend_worker: {
        grpc_client_config: {
          max_send_msg_size: $._config.grpc_server_max_msg_size,
        },
      },
      query_range: {
        align_queries_with_step: true,
        cache_results: true,
        max_retries: 5,
        results_cache: {
          cache: {
            memcached_client: {
              timeout: '500ms',
              consistent_hash: true,
              service: 'memcached-client',
              host: 'memcached-frontend.%s.svc.cluster.local' % $._config.namespace,
              update_interval: '1m',
              max_idle_conns: 16,
            },
          },
        },
      } + if $._config.queryFrontend.sharded_queries_enabled then {
        parallelise_shardable_queries: true,
      } else {},
      querier: {
        max_concurrent: $._config.querier.concurrency,
        query_ingesters_within: '2h',  // twice the max-chunk age (1h default) for safety buffer
      },
      limits_config: {
        // align middleware parallelism with shard factor to optimize one-legged sharded queries.
        max_query_parallelism: if $._config.queryFrontend.sharded_queries_enabled then
          // For a sharding factor of 16 (default), this is 256, or enough for 16 sharded queries.
          $._config.queryFrontend.shard_factor * 16
        else
          16,  // default to 16x parallelism
        reject_old_samples: true,
        reject_old_samples_max_age: '168h',
        max_query_length: '12000h',  // 500 days
        max_streams_per_user: 0,  // Disabled in favor of the global limit
        max_global_streams_per_user: 10000,  // 10k
        ingestion_rate_strategy: 'global',
        ingestion_rate_mb: 10,
        ingestion_burst_size_mb: 20,
        max_cache_freshness_per_query: '10m',
        split_queries_by_interval: '30m',
      },

      ingester: {
        chunk_idle_period: '15m',
        chunk_block_size: 262144,

        wal+: {
          enabled: true,
          dir: '/loki/wal',
          replay_memory_ceiling: '7GB',  // should be set upto ~50% of available memory
        },

        lifecycler: {
          ring: {
            heartbeat_timeout: '1m',
            replication_factor: $._config.replication_factor,
            kvstore: if $._config.memberlist_ring_enabled then {} else {
              store: 'consul',
              consul: {
                host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
                http_client_timeout: '20s',
                consistent_reads: true,
              },
            },
          },

          num_tokens: 512,
          heartbeat_period: '5s',
          join_after: '30s',
          interface_names: ['eth0'],
        },
      },
      pattern_ingester: {
        enabled: $._config.pattern_ingester.enabled,
        lifecycler: {
          ring: {
            heartbeat_timeout: '1m',
            replication_factor: 1,
            kvstore: if $._config.memberlist_ring_enabled then {} else {
              store: 'consul',
              consul: {
                host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
                http_client_timeout: '20s',
                consistent_reads: true,
              },
            },
          },

          num_tokens: 512,
          heartbeat_period: '5s',
          join_after: '30s',
          interface_names: ['eth0'],
        },
        client_config: {
          grpc_client_config: {
            max_recv_msg_size: 1024 * 1024 * 64,
          },
          remote_timeout: '1s',
        },
      },
      ingester_client: {
        grpc_client_config: {
          max_recv_msg_size: 1024 * 1024 * 64,
        },
        remote_timeout: '1s',
      },

      storage_config:
        {
          index_queries_cache_config: {
            memcached: {
              batch_size: 100,
              parallelism: 100,
            },
            memcached_client: {
              host: 'memcached-index-queries.%s.svc.cluster.local' % $._config.namespace,
              service: 'memcached-client',
              consistent_hash: true,
            },
          },
        } + $._config.object_store_config +
        (
          if $._config.use_thanos_objstore then {
            use_thanos_objstore: true,
            object_store: $._config.thanos_object_store_config,
          } else {}
        ),

      chunk_store_config: {
        chunk_cache_config: {
          memcached: {
            batch_size: 100,
            parallelism: 100,
          },

          memcached_client: {
            host: 'memcached.%s.svc.cluster.local' % $._config.namespace,
            service: 'memcached-client',
            consistent_hash: true,
          },
        },
      },

      schema_config: {
        configs: [{
          from: '2020-10-24',
          store: 'tsdb',
          object_store: $._config.storage_backend,
          schema: 'v13',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '%dh' % $._config.index_period_hours,
          },
        }],
      },

      distributor: {
        // Creates a ring between distributors, required by the ingestion rate global limit.
        ring: {
          kvstore: if $._config.memberlist_ring_enabled then {} else {
            store: 'consul',
            consul: {
              host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
              http_client_timeout: '20s',
              consistent_reads: false,
              watch_rate_limit: 1,
              watch_burst_size: 1,
            },
          },
        },
      },

      ruler: if $._config.ruler_enabled then {
        rule_path: '/tmp/rules',
        enable_api: true,
        alertmanager_url: 'http://alertmanager.%s.svc.cluster.local/alertmanager' % $._config.namespace,
        enable_sharding: true,
        enable_alertmanager_v2: true,
        ring: {
          kvstore: if $._config.memberlist_ring_enabled then {} else {
            store: 'consul',
            consul: {
              host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
            },
          },
        },
        storage+: {
          type: 'gcs',
          gcs+: {
            bucket_name: '%(cluster)s-%(namespace)s-ruler' % $._config,
          },
        },
      } else {},


    } + (
      if $._config.use_thanos_objstore && $._config.ruler_enabled then {
        ruler_storage: {
          backend: $._config.storage_backend,
        } + $._config.thanos_object_store_config,
      }
      else {}
    ),
  },

  local k = import 'ksonnet-util/kausal.libsonnet',
  local configMap = k.core.v1.configMap,

  config_file:
    configMap.new('loki') +
    configMap.withData({
      'config.yaml': k.util.manifestYaml($._config.loki),
    }),

  local deployment = k.apps.v1.deployment,

  config_hash_mixin::
    deployment.mixin.spec.template.metadata.withAnnotationsMixin({
      config_hash: std.md5(std.toString($._config.loki)),
    }),

}
