(import 'jaeger-agent-mixin/jaeger.libsonnet') +
(import 'images.libsonnet') +
(import 'common.libsonnet') +
(import 'config.libsonnet') +
(import 'overrides.libsonnet') +
(import 'consul/consul.libsonnet') +

// Loki services
(import 'distributor.libsonnet') +
(import 'ingester.libsonnet') +
(import 'querier.libsonnet') +
(import 'query-frontend.libsonnet') +
(import 'ruler.libsonnet') +

// Query scheduler support
// must be mixed in after frontend and querier so it can override their configuration.
(import 'query-scheduler.libsonnet') +

// Supporting services
(import 'memcached.libsonnet') +
(import 'overrides-exporter.libsonnet') +

// Index Gateway support
(import 'index-gateway.libsonnet') +

// BoltDB and TSDB Shipper support. Anything that modifies the compactor must be imported after this.
(import 'shipper.libsonnet') +

// Multi-zone ingester related config
(import 'multi-zone.libsonnet') +

// Memberlist related deployment configuration, mostly migration related
(import 'memberlist.libsonnet') +

// Prometheus ServiceMonitor
(import 'servicemonitor.libsonnet') +

// Patterns ingester
(import 'patterns.libsonnet')
