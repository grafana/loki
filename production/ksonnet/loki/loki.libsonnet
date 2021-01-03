(import 'ksonnet-util/kausal.libsonnet') +
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
(import 'table-manager.libsonnet') +
(import 'query-frontend.libsonnet') +
(import 'ruler.libsonnet') +

// Supporting services
(import 'memcached.libsonnet') +

// WAL support
(import 'wal.libsonnet') +

// BoltDB Shipper support. This should be the last one to get imported.
(import 'boltdb_shipper.libsonnet')
