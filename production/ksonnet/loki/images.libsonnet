{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.17-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.6.0',

    loki: 'grafana/loki:1.6.0',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
    tableManager: self.loki,
    query_frontend: self.loki,
    ruler: self.loki,
  },
}
