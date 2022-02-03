{
  auth_enabled: false,
  common: {
    path_prefix: '/var/loki',
    replication_factor: 3,
    ring: {
      kvstore: {
        store: 'memberlist',
      },
    },
    storage: {
      s3: {
        endpoint: 'minio:9000',
        insecure: true,
        bucketnames: 'loki-data',
        access_key_id: ' loki',
        secret_access_key: ' supersecret',
        s3forcepathstyle: true,
      },
    },
  },
  server: {
    http_listen_port: 3100,
  },
  ingester: {
    lifecycler: {
      final_sleep: '0s',
    },
  },
  memberlist: {
    join_members: [
      '{{ include "loki.fullname" . }}-memberlist',
    ],
  },
  schema_config: {
    configs: [
      {
        from: '2020-05-15',
        store: 'boltdb-shipper',
        object_store: 'filesystem',
        schema: 'v11',
        index: {
          prefix: 'index_',
          period: '24h',
        },
      },
    ],
  },
  limits_config: {
    enforce_metric_name: false,
    reject_old_samples: true,
    reject_old_samples_max_age: '168h',
    retention_period: '24h',
  },
  frontend: {
    log_queries_longer_than: '5s',
    compress_responses: true,
  },
  frontend_worker: {
    frontend_address: '{{ include "loki.queryFrontendFullname" . }}:9095',
    parallelism: 6,
    match_max_concurrent: true,
  },
  querier: {
    max_concurrent: 6,
  },
}
