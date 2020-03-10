(import 'ksonnet-util/kausal.libsonnet') +
(import 'ksonnet-util/jaeger.libsonnet') +
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

// Supporting services
(import 'memcached.libsonnet')
