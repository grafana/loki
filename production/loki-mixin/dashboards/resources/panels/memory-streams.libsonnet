// imports
local lib = import '../../../lib/_imports.libsonnet';
local config = import '../../../config.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

{
  new()::
    lib.panels.timeSeries.short({
      title: 'In-memory streams',
      description: |||
        The total number of in-memory streams per Ingester.
      |||,
      datasource: common.variables.metrics_datasource.name,
      targets: [
        lib.query.prometheus.new(
          datasource=common.variables.metrics_datasource.name,
          expr=shared.queries.ingester.memory_streams(),
          params={
            refId: 'streams',
            legendFormat: '{{%s}}' % [config.labels.pod],
          }
        ),
      ],
      fillOpacity: 10,
      showPoints: 'never',
      displayMode: 'table',
      calcs: ['max', 'mean', 'p95'],
      sortBy: '95th %',
      sortDesc: true,
    }),
}
