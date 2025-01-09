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
    shared.queries.queryScheduler.queue_duration_percentile_99,
    shared.queries.queryScheduler.queue_duration_percentile_90,
    shared.queries.queryScheduler.queue_duration_percentile_50,
    shared.queries.queryScheduler.queue_duration_average,
  ],
})
