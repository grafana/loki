// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

lib.panels.timeSeries.percent({
  title: 'Query Parsing (Sharded Engine)',
  description: |||
    The number of queries that were parsed by the Sharded Engine.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.queryFrontend.sharded_queries_percent,
      params={
        refId: 'parsed_queries',
        legendFormat: '{{type}}',
      }
    ),
  ],
  fillOpacity: 10,
  showPoints: 'never',
})
