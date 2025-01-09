// imports
local lib = import '../../../lib/_imports.libsonnet';
local common = import '../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;

lib.panels.timeSeries.bytes({
  title: 'Average Line Size',
  description: |||
    The average size of log lines for the Tenant.
  |||,
  datasource: common.variables.metrics_datasource.name,
  targets: [
    shared.queries.tenant.average_log_size,
  ],
})
