{
  _config+: {
    namespace: error 'must define namespace',
    cluster: error 'must define cluster',
    http_listen_port: 3100,

    replication_factor: 3,
    memcached_replicas: 3,

    grpc_server_max_msg_size: 100 << 20,  // 100MB

    // flag for tuning things when boltdb-shipper is current or upcoming index type.
    using_boltdb_shipper: true,

    wal_enabled: true,
    query_scheduler_enabled: false,
    overrides_exporter_enabled: false,

    // flags for running ingesters/queriers as a statefulset instead of deployment type.
    // WAL enabled configurations automatically use statefulsets.
    stateful_ingesters: false,
    ingester_pvc_size: '10Gi',
    ingester_pvc_class: 'fast',

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

    querier: {
      // This value should be set equal to (or less than) the CPU cores of the system the querier runs.
      // A higher value will lead to a querier trying to process more requests than there are available
      // cores and will result in scheduling delays.
      concurrency: 4,
    },

    queryFrontend: {
      replicas: 2,
      shard_factor: 16,  // v10 schema shard factor
      sharded_queries_enabled: false,
    },

    storage_backend: error 'must define storage_backend as a comma separated list of backends in use,\n    valid entries: dynamodb,s3,gcs,bigtable,cassandra. Typically this would be two entries, e.g. `gcs,bigtable`',

    enabledBackends: [
      backend
      for backend in std.split($._config.storage_backend, ',')
    ],

    table_prefix: $._config.namespace,
    index_period_hours: 24,  // 1 day

    ruler_enabled: false,

    // Bigtable variables
    bigtable_instance: error 'must specify bigtable instance',
    bigtable_project: error 'must specify bigtable project',

    // GCS variables
    gcs_bucket_name: error 'must specify GCS bucket name',

    // Cassandra variables
    cassandra_keyspace: 'lokiindex',
    cassandra_username: '',
    cassandra_password: '',
    cassandra_addresses: error 'must specify cassandra_addresses',

    // S3 variables
    s3_access_key: '',
    s3_secret_access_key: '',
    s3_address: error 'must specify s3_address',
    s3_bucket_name: error 'must specify s3_bucket_name',
    s3_path_style: false,

    // Dynamodb variables
    dynamodb_access_key: '',
    dynamodb_secret_access_key: '',
    dynamodb_region: error 'must specify dynamodb_region',

    client_configs: {
      dynamo: {
        dynamodb: {} + if $._config.dynamodb_access_key != '' then {
          dynamodb_url: 'dynamodb://' + $._config.dynamodb_access_key + ':' + $._config.dynamodb_secret_access_key + '@' + $._config.dynamodb_region,
        } else {
          dynamodb_url: 'dynamodb://' + $._config.dynamodb_region,
        },
      },
      s3: {
        s3forcepathstyle: $._config.s3_path_style,
      } + (
        if $._config.s3_access_key != '' then {
          s3: 's3://' + $._config.s3_access_key + ':' + $._config.s3_secret_access_key + '@' + $._config.s3_address + '/' + $._config.s3_bucket_name,
        } else {
          s3: 's3://' + $._config.s3_address + '/' + $._config.s3_bucket_name,
        }
      ),
      cassandra: {
        auth: false,
        addresses: $._config.cassandra_addresses,
        keyspace: $._config.cassandra_keyspace,
      } + (
        if $._config.cassandra_username != '' then {
          auth: true,
          username: $._config.cassandra_username,
          password: $._config.cassandra_password,
        } else {}
      ),
      gcp: {
        instance: $._config.bigtable_instance,
        project: $._config.bigtable_project,
      },
      gcs: {
        bucket_name: $._config.gcs_bucket_name,
      },
    },

    // December 11 is when we first launched to the public.
    // Assume we can ingest logs that are 5months old.
    schema_start_date: '2018-07-11',

    commonArgs: {
      'config.file': '/etc/loki/config/config.yaml',
      'limits.per-user-override-config': '/etc/loki/overrides/overrides.yaml',
    },

    loki: {
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
        match_max_concurrent: true,
        grpc_client_config: {
          max_send_msg_size: $._config.grpc_server_max_msg_size,
        },
      },
      query_range: {
        split_queries_by_interval: '30m',
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
        enforce_metric_name: false,
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
      },

      ingester: {
        chunk_idle_period: '15m',
        chunk_block_size: 262144,

        lifecycler: {
          ring: {
            heartbeat_timeout: '1m',
            replication_factor: $._config.replication_factor,
            kvstore: {
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

      ingester_client: {
        grpc_client_config: {
          max_recv_msg_size: 1024 * 1024 * 64,
        },
        remote_timeout: '1s',
      },

      storage_config: {
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
                      } +
                      (if std.count($._config.enabledBackends, 'gcs') > 0 then {
                         gcs: $._config.client_configs.gcs,
                       } else {}) +
                      (if std.count($._config.enabledBackends, 's3') > 0 then {
                         aws+: $._config.client_configs.s3,
                       } else {}) +
                      (if std.count($._config.enabledBackends, 'bigtable') > 0 then {
                         bigtable: $._config.client_configs.gcp,
                       } else {}) +
                      (if std.count($._config.enabledBackends, 'cassandra') > 0 then {
                         cassandra: $._config.client_configs.cassandra,
                       } else {}) +
                      (if std.count($._config.enabledBackends, 'dynamodb') > 0 then {
                         aws+: $._config.client_configs.dynamo,
                       } else {}),

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

        write_dedupe_cache_config: {
          memcached: {
            batch_size: 100,
            parallelism: 100,
          },

          memcached_client: {
            host: 'memcached-index-writes.%s.svc.cluster.local' % $._config.namespace,
            service: 'memcached-client',
            consistent_hash: true,
          },
        },
      },

      // Default schema config is boltdb-shipper/gcs, this will need to be overridden for other stores
      schema_config: {
        configs: [{
          from: '2020-10-24',
          store: 'boltdb-shipper',
          object_store: 'gcs',
          schema: 'v11',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '%dh' % $._config.index_period_hours,
          },
        }],
      },

      table_manager: {
        retention_period: 0,
        retention_deletes_enabled: false,
        poll_interval: '10m',
        creation_grace_period: '3h',
      },

      distributor: {
        // Creates a ring between distributors, required by the ingestion rate global limit.
        ring: {
          kvstore: {
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
          kvstore: {
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

    },
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
