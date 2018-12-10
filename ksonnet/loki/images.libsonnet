{
  _images+:: {
    // Various third-party images.
    memcached: 'memcached:1.5.6-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.4.1',

    // Our services.
    cortex_gw: 'raintank/cortex-gw:0.9.0-93-gceff250',
    tableManager: 'grafana/cortex-table-manager:r45-6247bbc8',

    loki: 'grafana/loki:master-d5e6c60',

    distributor: self.loki,
    ingester: self.loki,
    querier: self.loki,
  },
}
