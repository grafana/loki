{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.17-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.11.3',

    loki: 'grafana/loki:2.9.2',

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
