// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

timeSeries.cps({
  title: 'Lines Discarded',
  description: |||
    The total number of lines discarded per second by the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    lib.query.prometheus.new(
      common.variables.metrics_datasource.name,
      shared.queries.tenant.discarded_lines_rate,
      {
        refId: 'Discarded Lines Rate',
        legendFormat: 'Lines',
      }
    ),
  ],
})
