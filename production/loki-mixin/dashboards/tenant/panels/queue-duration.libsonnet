// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.seconds({
  title: 'Cell Queue Duration',
  description: |||
    The current global query scheduler queue duration

    Only applicable when [Shuffle Sharding](https://grafana.com/docs/loki/latest/operations/shuffle-sharding/) is enabled.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.queryScheduler.queue_duration_percentile_99,
      {
        refId: 'Queue Duration p99',
        legendFormat: 'p99',
      }
    ),
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.queryScheduler.queue_duration_percentile_95,
      {
        refId: 'Queue Duration p95',
        legendFormat: 'p95',
      }
    ),
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.queryScheduler.queue_duration_percentile_90,
      {
        refId: 'Queue Duration p90',
        legendFormat: 'p90',
      }
    ),
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.queryScheduler.queue_duration_percentile_50,
      {
        refId: 'Queue Duration p50',
        legendFormat: 'p50',
      }
    ),
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.queryScheduler.queue_duration_average,
      {
        refId: 'Queue Duration Average',
        legendFormat: 'avg',
      }
    ),
  ],
})
