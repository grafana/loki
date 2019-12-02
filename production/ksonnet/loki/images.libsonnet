{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.17-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.6.0',

    loki: 'grafana/loki:v1.0.0',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
    tableManager: self.loki,
  },
}
