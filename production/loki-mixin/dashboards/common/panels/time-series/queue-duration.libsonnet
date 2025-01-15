// imports
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../../shared/_imports.libsonnet';

// local variables
local percentiles = [99, 95, 90, 50];
local defaultTargets = std.flattenArrays(
    [
      [
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryScheduler['queue_duration_p' + p],
          params={
            refId: 'p' + p,
            legendFormat: 'p' + p,
          }
        )
      ]
      for p in percentiles
    ]
  )
  + [
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.queryScheduler.queue_duration_average,
      params={
        refId: 'avg',
        legendFormat: 'avg',
      }
    )
  ];

{
  new(
    title = 'Queue Duration',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.seconds({
    title: title,
    description: |||
      The current global query scheduler queue duration

      Only applicable when [Shuffle Sharding](https://grafana.com/docs/loki/latest/operations/shuffle-sharding/) is enabled.
    |||,
    datasource: common.variables.metrics_datasource.name,
    targets: targets,
  })
}
