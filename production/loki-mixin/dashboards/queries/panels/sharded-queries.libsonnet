// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

lib.panels.timeSeries.percent({
  title: 'Sharded Queries %',
  description: |||
    The percentage of queries that were sharded or bypassed by the Query Frontend.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.queryFrontend.sharded_queries_percent,
      params={
        refId: 'sharded',
        legendFormat: 'sharded',
      }
    ),
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.queryFrontend.sharding_bypassed_queries_percent,
      params={
        refId: 'bypassed',
        legendFormat: 'bypassed',
      }
    ),
  ],
  fillOpacity: 10,
  showPoints: 'never',
})
