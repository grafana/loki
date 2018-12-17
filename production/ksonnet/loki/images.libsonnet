{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.6-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.4.1',

    // Our services.
    tableManager: 'grafana/cortex-table-manager:r47-06f3294e',

    loki: 'grafana/loki:master',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
  },
}
