{
  _config+: {
    namespace: error 'must define namespace',
    cluster: error 'must define cluster',
    replication_factor: 3,

    memcached_replicas: 3,

    table_prefix: $._config.namespace,
    bigtable_instance: error 'must specify bigtable instance',
    bigtable_project: error 'must specify bigtable project',
    gcs_bucket_name: error 'must specify GCS bucket name',

    // December 11 is when we first launched to the public.
    // Assume we can ingest logs that are 5months old.
    schema_start_date: '2018-07-11',

    commonArgs: {
      'config.file': '/etc/loki/config.yaml',
    },

    ingester_client_config: {
      max_recv_msg_size: 1024 * 1024 * 64,
      remote_timeout: '1s',
    },

    loki: {
      server: {
        graceful_shutdown_timeout: '5s',
        http_server_idle_timeout: '120s',
        grpc_server_max_recv_msg_size: 1024 * 1024 * 64,
      },

      limits_config: {
        enforce_metric_name: false,
        reject_old_samples: true,
        reject_old_samples_max_age: '168h',
      },

      ingester: {
        chunk_idle_period: '15m',
        chunk_block_size: 262144,

        lifecycler: {
          ring: {
            heartbeat_timeout: '1m',
            replication_factor: 3,
            kvstore: {
              store: 'consul',
              consul: {
                host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
                prefix: '',
                httpclienttimeout: '20s',
                consistentreads: true,
              },
            },
          },

          num_tokens: 512,
          heartbeat_period: '5s',
          join_after: '10s',
          claim_on_rollout: false,
          interface_names: ['eth0'],
        },
      },

      storage_config: {
        bigtable: {
          instance: $._config.bigtable_instance,
          project: $._config.bigtable_project,
        },
        gcs: {
          bucket_name: $._config.gcs_bucket_name,
        },

        index_queries_cache_config: {
          memcached: {
            batch_size: 100,
            parallelism: 100,
          },

          memcached_client: {
            host: 'memcached-index-queries.%s.svc.cluster.local' % $._config.namespace,
            service: 'memcached-client',
          },
        },
      },

      chunk_store_config: {
        chunk_cache_config: {
          memcached: {
            batch_size: 100,
            parallelism: 100,
          },

          memcached_client: {
            host: 'memcached.%s.svc.cluster.local' % $._config.namespace,
            service: 'memcached-client',
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
          },
        },
        max_look_back_period: 0,
      },

      schema_config: {
        configs: [{
          from: '2018-04-15',
          store: 'bigtable',
          object_store: 'gcs',
          schema: 'v9',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '168h',
          },
        }],
      },

      table_manager: {
        retention_period: 0,
        retention_deletes_enabled: false,
        index_tables_provisioning: {
          inactive_read_throughput: 0,
          inactive_write_throughput: 0,
          provisioned_read_throughput: 0,
          provisioned_write_throughput: 0,
        },
        chunk_tables_provisioning: {
          inactive_read_throughput: 0,
          inactive_write_throughput: 0,
          provisioned_read_throughput: 0,
          provisioned_write_throughput: 0,
        },
      },
    },
  },

  local configMap = $.core.v1.configMap,

  config_file:
    configMap.new('loki') +
    configMap.withData({
      'config.yaml': $.util.manifestYaml($._config.loki),
    }),

  local deployment = $.apps.v1beta1.deployment,

  config_hash_mixin::
    deployment.mixin.spec.template.metadata.withAnnotationsMixin({
      config_hash: std.md5(std.toString($._config.loki)),
    }),
}
