{
  _images+:: {
    // Various third-party images.
    consul: 'consul:0.8.5',
    consulSidekick: 'quay.io/weaveworks/consul-sidekick:master-f18ad13',
    statsdExporter: 'prom/statsd-exporter:v0.6.0',
    consulExporter: 'prom/consul-exporter:v0.3.0',
    memcached: 'memcached:1.5.6-alpine',
    memcachedExporter: 'prom/memcached-exporter:v0.4.1',

    // Our services.
    cortex_gw: 'raintank/cortex-gw:0.9.0-93-gceff250',
    tableManager: 'grafana/cortex-table-manager:r45-6247bbc8',
    tempo: 'grafana/tempo:master-4635769-WIP',
  },
}
