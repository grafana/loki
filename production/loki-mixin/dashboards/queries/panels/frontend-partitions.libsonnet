// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local percentiles = [99, 95, 90, 50];
local targets = std.flattenArrays(
    [
      [
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.queryFrontend['partitions_p' + p],
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
      expr=shared.queries.queryFrontend.partitions_average,
      params={
        refId: 'avg',
        legendFormat: 'avg',
      }
    )
  ];

lib.panels.timeSeries.seconds({
  title: 'Intervals Per Query',
  description: |||
    The number of partitions that were processed by the Query Frontend.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: targets,
  fillOpacity: 10,
  showPoints: 'never',
})
