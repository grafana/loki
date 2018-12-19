{
  _config+: {
    namespace: error 'must define namespace',
    cluster: error 'must define cluster',
    replication_factor: 3,

    storage_backend: error 'must specify storage backend (cassandra, gcp, aws)',
    table_prefix: $._config.namespace,
    cassandra_addresses: error 'must specify cassandra addresses',
    bigtable_instance: error 'must specify bigtable instance',
    bigtable_project: error 'must specify bigtable project',
    gcs_bucket_name: error 'must specify GCS bucket name',
    aws_region: error 'must specify AWS region',
    s3_bucket_name: error 'must specify S3 bucket name',

    // December 11 is when we first launched to the public.
    // Assume we can ingest logs that are 5months old.
    schema_start_date: '2018-07-11',

    server_config: {
      http_listen_port: 80,
      grpc_listen_port: 9095,

      graceful_shutdown_timeout: '5s',
      http_server_read_timeout: '30s',
      http_server_write_timeout: '30s',
      http_server_idle_timeout: '120s',

      grpc_server_max_recv_msg_size: 1024 * 1024 * 64,

      log_level: 'info',
    },

    ingester_client_config: {
      max_recv_msg_size: 1024 * 1024 * 64,
      remote_timeout: '1s',
    },

    ingester_config: {
      lifecycler: {
        ring: {
          store: 'consul',
          heartbeat_timeout: '1m',
          replication_factor: 3,

          consul: {
            host: 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
            prefix: '',
            httpclienttimeout: '20s',
            consistentreads: true,
          },
        },

        num_tokens: 512,
        heartbeat_period: '5s',
        join_after: '10s',
        claim_on_rollout: false,
        interface_names: ['eth0'],
      },
    },

    local awsStorageConfig(config) = {
      aws: {
        // TODO.
      },
    },

    local gcpStorageConfig(config) = {
      bigtable: {
        instance: config.bigtable_instance,
        project: config.bigtable_project,
      },
      gcs: {
        bucket_name: config.gcs_bucket_name,
      },
    },

    storage_config:
      if $._config.storage_backend == 'aws' then awsStorageConfig($._config)
      else if $._config.storage_backend == 'gcp' then gcpStorageConfig($._config)
      else {},

    local awsSchemaConfig(config) = [{
      from: '0',
      store: 'bigtable',
      object_store: 'gcs',
      schema: 'v9',
      index: {
        prefix: '%s_index_' % config.table_prefix,
        period: '168h',
      },
    }],

    local gcpSchemaConfig(config) = [{
      from: '0',
      store: 'bigtable',
      object_store: 'gcs',
      schema: 'v9',
      index: {
        prefix: '%s_index_' % config.table_prefix,
        period: '168h',
      },
    }],

    schema_configs:
      if $._config.storage_backend == 'aws' then awsSchemaConfig($._config)
      else if $._config.storage_backend == 'gcp' then gcpSchemaConfig($._config)
      else {},

    commonArgs: {
      'config.file': '/etc/loki/config.yaml',
    },
  },

  local configMap = $.core.v1.configMap,

  config_file:
    configMap.new('loki') +
    configMap.withData({
      'config.yaml': $.util.manifestYaml({
        server: $._config.server_config,
        auth_enabled: true,
        ingester: $._config.ingester_config,
        storage_config: $._config.storage_config,
        schema_config: {
          configs: $._config.schema_configs,
        },
      }),
    }),
}
