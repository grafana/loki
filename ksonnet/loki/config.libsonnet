{
  _config+: {
    namespace: error 'must define namespace',
    replication_factor: 3,

    ringConfig: {
      'consul.hostname': 'consul.%s.svc.cluster.local:8500' % $._config.namespace,
      'consul.prefix': '',
    },
  },
}
