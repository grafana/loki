{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.6-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.4.1',

    loki: 'grafana/loki:v0.3.0',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
    tableManager: self.loki,
  },
}
