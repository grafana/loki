{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.6.38-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.15.2',

    loki: 'grafana/loki:3.5.1',

    distributor:: self.loki,
    ingester:: self.loki,
    pattern_ingester:: self.loki,
    querier:: self.loki,
    query_frontend:: self.loki,
    query_scheduler:: self.loki,
    ruler:: self.loki,
    compactor:: self.loki,
    index_gateway:: self.loki,
    overrides_exporter:: self.loki,
    bloom_gateway:: self.loki,
    bloom_compactor:: self.loki,
  },
}
