// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

lib.panels.timeSeries.short({
  title: 'Active Streams',
  description: |||
    The total number of active streams for the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.active_streams,
      {
        refId: 'Active Streams',
        legendFormat: 'Streams',
      }
    ),
  ],
})
