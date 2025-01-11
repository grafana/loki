// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.short({
  title: 'Queue Length',
  description: |||
    The query scheduler queue length for the Tenant.

    Only applicable when [Shuffle Sharding](https://grafana.com/docs/loki/latest/operations/shuffle-sharding/) is enabled.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      datasource=common.variables.metrics_datasource.name,
      expr=shared.queries.queryScheduler.queue_length,
      params={
        refId: 'Active Streams',
        legendFormat: 'Streams',
      }
    ),
  ],
})
