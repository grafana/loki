{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.6-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.4.1',

    // Our services.
    tableManager: 'grafana/cortex-table-manager:r56-bd83f04a',

    loki: 'grafana/loki:latest',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
  },
}
